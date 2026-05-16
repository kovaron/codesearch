function greet(name: string): string {
    return `Hello, ${name}`;
}

class UserService {
    getUser(id: number) {
        return { id, name: "Alice" };
    }

    deleteUser(id: number): void {
        console.log(`Deleting user ${id}`);
    }
}

const formatEmail = (email: string): string => email.toLowerCase();
