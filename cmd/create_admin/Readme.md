Compiles cleanly. Two ways to use it:

Create a new admin user:
go run cmd/create_admin/main.go -email admin@example.com -password secret123 -name "Admin User"

Promote an existing user to admin:
go run cmd/create_admin/main.go -promote user1@gmail.com

✓ Admin user created successfully!
ID: 69db2c25927755d6fc18a060
Email: admin@todayintech.in
Name: Admin User
Role: admin
pass: Test@123
