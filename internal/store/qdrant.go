package store

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"time"

	"github.com/kovaron/codesearch/internal/parser"
	"github.com/qdrant/go-client/qdrant"
)

const heartbeatID uint64 = 0
const heartbeatNodeType = "__heartbeat__"

// QdrantStore implements Store using Qdrant gRPC.
type QdrantStore struct {
	client     *qdrant.Client
	collection string
	dim        uint64
}

// NewQdrant creates a new QdrantStore, connecting to the given host:port and
// ensuring the collection exists with the given vector dimension.
func NewQdrant(ctx context.Context, host string, port int, project string, dim int) (*QdrantStore, error) {
	client, err := qdrant.NewClient(&qdrant.Config{
		Host:                   host,
		Port:                   port,
		SkipCompatibilityCheck: true,
	})
	if err != nil {
		return nil, fmt.Errorf("qdrant client: %w", err)
	}

	s := &QdrantStore{
		client:     client,
		collection: project,
		dim:        uint64(dim),
	}

	if err := s.ensureCollection(ctx); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *QdrantStore) ensureCollection(ctx context.Context) error {
	exists, err := s.client.CollectionExists(ctx, s.collection)
	if err != nil {
		return fmt.Errorf("check collection: %w", err)
	}
	if exists {
		return nil
	}
	return s.client.CreateCollection(ctx, &qdrant.CreateCollection{
		CollectionName: s.collection,
		VectorsConfig: qdrant.NewVectorsConfig(&qdrant.VectorParams{
			Size:     s.dim,
			Distance: qdrant.Distance_Cosine,
		}),
	})
}

// chunkID computes a deterministic uint64 from path, node_type, and start_byte.
// If the result collides with the reserved heartbeat ID (0), bump to 1.
func chunkID(path, nodeType string, startByte int) uint64 {
	h := sha256.New()
	h.Write([]byte(path))
	h.Write([]byte("|"))
	h.Write([]byte(nodeType))
	h.Write([]byte("|"))
	b := make([]byte, 8)
	binary.LittleEndian.PutUint64(b, uint64(startByte))
	h.Write(b)
	sum := h.Sum(nil)
	id := binary.LittleEndian.Uint64(sum[:8])
	if id == heartbeatID {
		id = 1
	}
	return id
}

// Upsert stores a chunk with its vector in the collection.
func (s *QdrantStore) Upsert(ctx context.Context, path string, chunk parser.Chunk, vector []float32) error {
	id := chunkID(path, chunk.NodeType, chunk.StartByte)
	payload := qdrant.NewValueMap(map[string]any{
		"filepath":   path,
		"name":       chunk.Name,
		"node_type":  chunk.NodeType,
		"language":   chunk.Language,
		"start_line": int64(chunk.StartLine),
		"end_line":   int64(chunk.EndLine),
		"text":       chunk.Text,
	})
	_, err := s.client.Upsert(ctx, &qdrant.UpsertPoints{
		CollectionName: s.collection,
		Points: []*qdrant.PointStruct{
			{
				Id:      qdrant.NewIDNum(id),
				Vectors: qdrant.NewVectors(vector...),
				Payload: payload,
			},
		},
	})
	return err
}

// DeleteByFile removes all points with filepath == path.
func (s *QdrantStore) DeleteByFile(ctx context.Context, path string) error {
	_, err := s.client.Delete(ctx, &qdrant.DeletePoints{
		CollectionName: s.collection,
		Points: &qdrant.PointsSelector{
			PointsSelectorOneOf: &qdrant.PointsSelector_Filter{
				Filter: &qdrant.Filter{
					Must: []*qdrant.Condition{
						qdrant.NewMatchKeyword("filepath", path),
					},
				},
			},
		},
	})
	return err
}

// SearchSemantic performs a vector similarity search, filtering out heartbeat points.
func (s *QdrantStore) SearchSemantic(ctx context.Context, vector []float32, limit int) ([]SearchResult, error) {
	results, err := s.client.Query(ctx, &qdrant.QueryPoints{
		CollectionName: s.collection,
		Query:          qdrant.NewQuery(vector...),
		Limit:          qdrant.PtrOf(uint64(limit)),
		WithPayload:    qdrant.NewWithPayload(true),
		Filter: &qdrant.Filter{
			MustNot: []*qdrant.Condition{
				qdrant.NewMatchKeyword("node_type", heartbeatNodeType),
			},
		},
	})
	if err != nil {
		return nil, err
	}
	return scoredPointsToResults(results), nil
}

// SearchStructural filters by name/node_type/language, skipping empty values.
func (s *QdrantStore) SearchStructural(ctx context.Context, name, nodeType, language string, limit int) ([]SearchResult, error) {
	var must []*qdrant.Condition
	if name != "" {
		must = append(must, qdrant.NewMatchKeyword("name", name))
	}
	if nodeType != "" {
		must = append(must, qdrant.NewMatchKeyword("node_type", nodeType))
	}
	if language != "" {
		must = append(must, qdrant.NewMatchKeyword("language", language))
	}
	// Always exclude heartbeat
	mustNot := []*qdrant.Condition{
		qdrant.NewMatchKeyword("node_type", heartbeatNodeType),
	}

	points, err := s.client.Scroll(ctx, &qdrant.ScrollPoints{
		CollectionName: s.collection,
		Limit:          qdrant.PtrOf(uint32(limit)),
		WithPayload:    qdrant.NewWithPayload(true),
		Filter: &qdrant.Filter{
			Must:    must,
			MustNot: mustNot,
		},
	})
	if err != nil {
		return nil, err
	}
	return retrievedPointsToResults(points), nil
}

// ListByPath returns points whose filepath matches pathPrefix (text/keyword match).
func (s *QdrantStore) ListByPath(ctx context.Context, pathPrefix string, limit int) ([]SearchResult, error) {
	points, err := s.client.Scroll(ctx, &qdrant.ScrollPoints{
		CollectionName: s.collection,
		Limit:          qdrant.PtrOf(uint32(limit)),
		WithPayload:    qdrant.NewWithPayload(true),
		Filter: &qdrant.Filter{
			Must: []*qdrant.Condition{
				qdrant.NewMatchText("filepath", pathPrefix),
			},
			MustNot: []*qdrant.Condition{
				qdrant.NewMatchKeyword("node_type", heartbeatNodeType),
			},
		},
	})
	if err != nil {
		return nil, err
	}
	return retrievedPointsToResults(points), nil
}

// GetByName returns the single point matching (path, name), or nil if not found.
func (s *QdrantStore) GetByName(ctx context.Context, path, name string) (*SearchResult, error) {
	points, err := s.client.Scroll(ctx, &qdrant.ScrollPoints{
		CollectionName: s.collection,
		Limit:          qdrant.PtrOf(uint32(1)),
		WithPayload:    qdrant.NewWithPayload(true),
		Filter: &qdrant.Filter{
			Must: []*qdrant.Condition{
				qdrant.NewMatchKeyword("filepath", path),
				qdrant.NewMatchKeyword("name", name),
			},
		},
	})
	if err != nil {
		return nil, err
	}
	if len(points) == 0 {
		return nil, nil
	}
	results := retrievedPointsToResults(points)
	if len(results) == 0 {
		return nil, nil
	}
	return &results[0], nil
}

// WriteHeartbeat stores a heartbeat point at reserved ID 0.
func (s *QdrantStore) WriteHeartbeat(ctx context.Context) error {
	now := time.Now().Unix()
	payload := qdrant.NewValueMap(map[string]any{
		"node_type": heartbeatNodeType,
		"last_seen": now,
	})
	_, err := s.client.Upsert(ctx, &qdrant.UpsertPoints{
		CollectionName: s.collection,
		Points: []*qdrant.PointStruct{
			{
				Id:      qdrant.NewIDNum(heartbeatID),
				Vectors: qdrant.NewVectors(make([]float32, s.dim)...),
				Payload: payload,
			},
		},
	})
	return err
}

// HeartbeatAge returns seconds since the last heartbeat, or -1 if missing.
func (s *QdrantStore) HeartbeatAge(ctx context.Context) (int64, error) {
	points, err := s.client.Get(ctx, &qdrant.GetPoints{
		CollectionName: s.collection,
		Ids:            []*qdrant.PointId{qdrant.NewIDNum(heartbeatID)},
		WithPayload:    qdrant.NewWithPayload(true),
	})
	if err != nil {
		return -1, err
	}
	if len(points) == 0 {
		return -1, nil
	}
	p := points[0]
	if p.Payload == nil {
		return -1, nil
	}
	v, ok := p.Payload["last_seen"]
	if !ok {
		return -1, nil
	}
	lastSeen := v.GetIntegerValue()
	return time.Now().Unix() - lastSeen, nil
}

// scoredPointsToResults converts ScoredPoint slices from Query results.
func scoredPointsToResults(points []*qdrant.ScoredPoint) []SearchResult {
	results := make([]SearchResult, 0, len(points))
	for _, p := range points {
		results = append(results, payloadToResult(p.Payload, p.Score))
	}
	return results
}

// retrievedPointsToResults converts RetrievedPoint slices from Scroll/Get results.
func retrievedPointsToResults(points []*qdrant.RetrievedPoint) []SearchResult {
	results := make([]SearchResult, 0, len(points))
	for _, p := range points {
		results = append(results, payloadToResult(p.Payload, 0))
	}
	return results
}

func payloadToResult(payload map[string]*qdrant.Value, score float32) SearchResult {
	return SearchResult{
		Filepath:  strVal(payload, "filepath"),
		Name:      strVal(payload, "name"),
		NodeType:  strVal(payload, "node_type"),
		Language:  strVal(payload, "language"),
		StartLine: int(intVal(payload, "start_line")),
		EndLine:   int(intVal(payload, "end_line")),
		Text:      strVal(payload, "text"),
		Score:     score,
	}
}

func strVal(payload map[string]*qdrant.Value, key string) string {
	if payload == nil {
		return ""
	}
	v, ok := payload[key]
	if !ok || v == nil {
		return ""
	}
	return v.GetStringValue()
}

func intVal(payload map[string]*qdrant.Value, key string) int64 {
	if payload == nil {
		return 0
	}
	v, ok := payload[key]
	if !ok || v == nil {
		return 0
	}
	return v.GetIntegerValue()
}
