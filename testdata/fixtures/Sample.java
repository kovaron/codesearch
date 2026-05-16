public class UserService {
    public String getUser(int id) {
        return "User " + id;
    }

    private void validateId(int id) {
        if (id <= 0) throw new IllegalArgumentException("Invalid ID");
    }
}

interface Repository {
    String findById(int id);
}
