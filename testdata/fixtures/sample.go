package fixtures

import "fmt"

func Add(a, b int) int {
	return a + b
}

func Greet(name string) string {
	return fmt.Sprintf("Hello, %s", name)
}

type Rectangle struct {
	Width  float64
	Height float64
}

func (r *Rectangle) Area() float64 {
	return r.Width * r.Height
}

func (r *Rectangle) Perimeter() float64 {
	return 2 * (r.Width + r.Height)
}
