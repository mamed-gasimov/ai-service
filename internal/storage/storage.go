package storage

import (
	"context"
	"io"
)

// Storage defines an abstraction over object-storage backends (S3 / MinIO / etc.).
type Storage interface {
	// Upload streams the content from reader directly into the bucket.
	Upload(ctx context.Context, objectKey string, reader io.Reader, size int64, contentType string) error

	// Download returns a ReadCloser for the object content. The caller must close it.
	Download(ctx context.Context, objectKey string) (io.ReadCloser, error)

	// Delete removes the object by key.
	Delete(ctx context.Context, objectKey string) error

	// EnsureBucket creates the bucket if it does not exist.
	EnsureBucket(ctx context.Context, bucket string) error
}
