# Use the official Golang image with version 1.20
FROM golang:1.20.0

# Set the working directory in the container
WORKDIR /server

# Copy go.mod and go.sum to download dependencies
COPY go.mod go.sum ./

# Download Go dependencies
RUN go mod download

# Copy the application source code to the container
COPY . .

# Build the Go application
RUN go build -o /bin/main server/main.go

# Set the command to run the Go application
CMD ["/bin/main"]

# Expose port 80 for the application to listen on
EXPOSE 80
