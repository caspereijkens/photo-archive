## Testing
1. Replace nginx with localhost in main.go
2. `sudo docker compose build`
3. `sudo docker compose up -d minio1 minio2 minio3 minio4 nginx db`
4. `cd server`
5. comment out init from main.go
5. `go test`

```bash
sudo docker compose exec db psql -U postgres
postgres=# CREATE DATABASE joepeijkens;
CREATE USER <user> WITH PASSWORD '<password>';
```

# TODO 
- FEATURE: improve testing setup so I don't have to change the code to make it localhost.
- FEATURE: package code