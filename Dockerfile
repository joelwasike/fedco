# Use the official Go image
FROM golang:1.23.1-alpine

# Set the working directory inside the container
WORKDIR /app

# Copy go.mod and go.sum
COPY go.mod go.sum ./

# Download Go dependencies
RUN go mod download

# Copy the source code
COPY . .

# Build the Go application
RUN go build -o fedco .

# Expose the port the app runs on
EXPOSE 8080

# Run the application
CMD ["./fedco"]
