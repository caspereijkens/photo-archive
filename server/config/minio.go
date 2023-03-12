package config

import "github.com/minio/minio-go"

func NewMinIO() (*minio.Client, error) {
	endpoint := "nginx:9000"
	accessKeyID := "minioadmin"
	secretAccessKey := "minioadmin"
	useSSL := false
	minioClient, err := minio.New(endpoint, accessKeyID, secretAccessKey, useSSL)
	return minioClient, err
}
