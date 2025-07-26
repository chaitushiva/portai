go build -o kairo-agent main.go
go mod init kairo-agent
./kairo-agent --api-url=http://localhost:8000/analyze


