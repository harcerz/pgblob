package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// BlobStorage defines the interface for blob storage backends
type BlobStorage interface {
	Download(ctx context.Context, dbName string) (io.ReadCloser, error)
	Upload(ctx context.Context, dbName string, data io.Reader) error
	List(ctx context.Context) ([]string, error)
	Delete(ctx context.Context, dbName string) error
	Exists(ctx context.Context, dbName string) (bool, error)
}

// NewBlobStorage creates a new blob storage backend based on configuration
func NewBlobStorage(cfg *Config) (BlobStorage, error) {
	switch cfg.Storage.Backend {
	case "local":
		return NewLocalStorage(cfg.Storage.Local.BasePath)
	case "s3":
		return NewS3Storage(cfg.Storage.S3)
	case "azure":
		return NewAzureStorage(cfg.Storage.Azure)
	default:
		return nil, fmt.Errorf("unsupported storage backend: %s", cfg.Storage.Backend)
	}
}

// LocalStorage implements BlobStorage for local filesystem
type LocalStorage struct {
	basePath string
}

// NewLocalStorage creates a new local storage backend
func NewLocalStorage(basePath string) (*LocalStorage, error) {
	// Create directory if it doesn't exist
	if err := os.MkdirAll(basePath, 0755); err != nil {
		return nil, fmt.Errorf("failed to create storage directory: %w", err)
	}
	return &LocalStorage{basePath: basePath}, nil
}

func (s *LocalStorage) Download(ctx context.Context, dbName string) (io.ReadCloser, error) {
	path := filepath.Join(s.basePath, dbName+".sqlite")
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // Database doesn't exist yet
		}
		return nil, fmt.Errorf("failed to open database file: %w", err)
	}
	return file, nil
}

func (s *LocalStorage) Upload(ctx context.Context, dbName string, data io.Reader) error {
	path := filepath.Join(s.basePath, dbName+".sqlite")

	// Create temporary file
	tmpPath := path + ".tmp"
	tmpFile, err := os.Create(tmpPath)
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	defer tmpFile.Close()

	// Copy data to temp file
	if _, err := io.Copy(tmpFile, data); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("failed to write to temp file: %w", err)
	}

	// Sync to disk
	if err := tmpFile.Sync(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("failed to sync temp file: %w", err)
	}
	tmpFile.Close()

	// Rename to final location (atomic on POSIX)
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("failed to rename temp file: %w", err)
	}

	return nil
}

func (s *LocalStorage) List(ctx context.Context) ([]string, error) {
	entries, err := os.ReadDir(s.basePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read directory: %w", err)
	}

	var databases []string
	for _, entry := range entries {
		if !entry.IsDir() && filepath.Ext(entry.Name()) == ".sqlite" {
			name := entry.Name()[:len(entry.Name())-7] // Remove .sqlite extension
			databases = append(databases, name)
		}
	}
	return databases, nil
}

func (s *LocalStorage) Delete(ctx context.Context, dbName string) error {
	path := filepath.Join(s.basePath, dbName+".sqlite")
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete database: %w", err)
	}
	return nil
}

func (s *LocalStorage) Exists(ctx context.Context, dbName string) (bool, error) {
	path := filepath.Join(s.basePath, dbName+".sqlite")
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

// S3Storage implements BlobStorage for AWS S3
type S3Storage struct {
	client *s3.Client
	bucket string
	prefix string
}

// NewS3Storage creates a new S3 storage backend
func NewS3Storage(cfg S3Config) (*S3Storage, error) {
	ctx := context.Background()

	// Load AWS config
	awsCfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(cfg.Region))
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	return &S3Storage{
		client: s3.NewFromConfig(awsCfg),
		bucket: cfg.Bucket,
		prefix: cfg.Prefix,
	}, nil
}

func (s *S3Storage) getKey(dbName string) string {
	return s.prefix + dbName + ".sqlite"
}

func (s *S3Storage) Download(ctx context.Context, dbName string) (io.ReadCloser, error) {
	key := s.getKey(dbName)

	result, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		// Check if object doesn't exist
		if isS3NotFound(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to download from S3: %w", err)
	}

	return result.Body, nil
}

func (s *S3Storage) Upload(ctx context.Context, dbName string, data io.Reader) error {
	key := s.getKey(dbName)

	_, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
		Body:   data,
	})
	if err != nil {
		return fmt.Errorf("failed to upload to S3: %w", err)
	}

	return nil
}

func (s *S3Storage) List(ctx context.Context) ([]string, error) {
	result, err := s.client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
		Bucket: aws.String(s.bucket),
		Prefix: aws.String(s.prefix),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list S3 objects: %w", err)
	}

	var databases []string
	for _, obj := range result.Contents {
		key := *obj.Key
		if len(key) > len(s.prefix)+7 { // prefix + name + .sqlite
			name := key[len(s.prefix) : len(key)-7]
			databases = append(databases, name)
		}
	}
	return databases, nil
}

func (s *S3Storage) Delete(ctx context.Context, dbName string) error {
	key := s.getKey(dbName)

	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return fmt.Errorf("failed to delete from S3: %w", err)
	}

	return nil
}

func (s *S3Storage) Exists(ctx context.Context, dbName string) (bool, error) {
	key := s.getKey(dbName)

	_, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		if isS3NotFound(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func isS3NotFound(err error) bool {
	// Check if error is NoSuchKey or NotFound
	return err != nil && (err.Error() == "NoSuchKey" || err.Error() == "NotFound")
}

// AzureStorage implements BlobStorage for Azure Blob Storage
type AzureStorage struct {
	client    *azblob.Client
	container string
}

// NewAzureStorage creates a new Azure storage backend
func NewAzureStorage(cfg AzureConfig) (*AzureStorage, error) {
	var client *azblob.Client

	accountURL := fmt.Sprintf("https://%s.blob.core.windows.net/", cfg.Account)

	if cfg.UseManagedIdentity {
		// Use managed identity
		credential, err := azidentity.NewDefaultAzureCredential(nil)
		if err != nil {
			return nil, fmt.Errorf("failed to create Azure credential: %w", err)
		}
		client, err = azblob.NewClient(accountURL, credential, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to create Azure client: %w", err)
		}
	} else {
		// Use account key
		credential, err := azblob.NewSharedKeyCredential(cfg.Account, cfg.Key)
		if err != nil {
			return nil, fmt.Errorf("failed to create Azure credential: %w", err)
		}
		var clientErr error
		client, clientErr = azblob.NewClientWithSharedKeyCredential(accountURL, credential, nil)
		if clientErr != nil {
			return nil, fmt.Errorf("failed to create Azure client: %w", clientErr)
		}
	}

	return &AzureStorage{
		client:    client,
		container: cfg.Container,
	}, nil
}

func (s *AzureStorage) getBlobName(dbName string) string {
	return dbName + ".sqlite"
}

func (s *AzureStorage) Download(ctx context.Context, dbName string) (io.ReadCloser, error) {
	blobName := s.getBlobName(dbName)

	// Check if blob exists first
	blobClient := s.client.ServiceClient().NewContainerClient(s.container).NewBlobClient(blobName)
	_, err := blobClient.GetProperties(ctx, nil)
	if err != nil {
		// Blob doesn't exist
		return nil, nil
	}

	response, err := blobClient.DownloadStream(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to download from Azure: %w", err)
	}

	return response.Body, nil
}

func (s *AzureStorage) Upload(ctx context.Context, dbName string, data io.Reader) error {
	blobName := s.getBlobName(dbName)
	blobClient := s.client.ServiceClient().NewContainerClient(s.container).NewBlockBlobClient(blobName)

	_, err := blobClient.UploadStream(ctx, data, nil)
	if err != nil {
		return fmt.Errorf("failed to upload to Azure: %w", err)
	}

	return nil
}

func (s *AzureStorage) List(ctx context.Context) ([]string, error) {
	containerClient := s.client.ServiceClient().NewContainerClient(s.container)

	pager := containerClient.NewListBlobsFlatPager(nil)
	var databases []string

	for pager.More() {
		resp, err := pager.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to list Azure blobs: %w", err)
		}

		for _, blob := range resp.Segment.BlobItems {
			if blob.Name != nil {
				name := *blob.Name
				if len(name) > 7 && name[len(name)-7:] == ".sqlite" {
					databases = append(databases, name[:len(name)-7])
				}
			}
		}
	}

	return databases, nil
}

func (s *AzureStorage) Delete(ctx context.Context, dbName string) error {
	blobName := s.getBlobName(dbName)
	blobClient := s.client.ServiceClient().NewContainerClient(s.container).NewBlobClient(blobName)

	_, err := blobClient.Delete(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to delete from Azure: %w", err)
	}

	return nil
}

func (s *AzureStorage) Exists(ctx context.Context, dbName string) (bool, error) {
	blobName := s.getBlobName(dbName)
	blobClient := s.client.ServiceClient().NewContainerClient(s.container).NewBlobClient(blobName)

	_, err := blobClient.GetProperties(ctx, nil)
	if err != nil {
		// Check if blob doesn't exist
		return false, nil
	}
	return true, nil
}

// DatabaseCache manages local caching of database files
type DatabaseCache struct {
	storage      BlobStorage
	localPath    string
	lastSync     time.Time
	ttlMinutes   int
	dbName       string
}

// NewDatabaseCache creates a new database cache
func NewDatabaseCache(storage BlobStorage, dbName string, ttlMinutes int) *DatabaseCache {
	return &DatabaseCache{
		storage:    storage,
		localPath:  filepath.Join(os.TempDir(), fmt.Sprintf("%s-%d.sqlite", dbName, time.Now().Unix())),
		ttlMinutes: ttlMinutes,
		dbName:     dbName,
	}
}

// GetLocalPath returns the local path to the cached database
func (c *DatabaseCache) GetLocalPath() string {
	return c.localPath
}

// Download downloads the database from blob storage to local cache
func (c *DatabaseCache) Download(ctx context.Context) error {
	reader, err := c.storage.Download(ctx, c.dbName)
	if err != nil {
		return fmt.Errorf("failed to download database: %w", err)
	}

	// If database doesn't exist in storage, create an empty one
	if reader == nil {
		// Create empty file
		file, err := os.Create(c.localPath)
		if err != nil {
			return fmt.Errorf("failed to create empty database: %w", err)
		}
		file.Close()
		c.lastSync = time.Now()
		return nil
	}
	defer reader.Close()

	// Write to local file
	file, err := os.Create(c.localPath)
	if err != nil {
		return fmt.Errorf("failed to create local database file: %w", err)
	}
	defer file.Close()

	if _, err := io.Copy(file, reader); err != nil {
		return fmt.Errorf("failed to write database to local file: %w", err)
	}

	c.lastSync = time.Now()
	return nil
}

// Upload uploads the database from local cache to blob storage
func (c *DatabaseCache) Upload(ctx context.Context) error {
	file, err := os.Open(c.localPath)
	if err != nil {
		return fmt.Errorf("failed to open local database file: %w", err)
	}
	defer file.Close()

	if err := c.storage.Upload(ctx, c.dbName, file); err != nil {
		return fmt.Errorf("failed to upload database: %w", err)
	}

	c.lastSync = time.Now()
	return nil
}

// ShouldSync returns true if the cache should be synced based on TTL
func (c *DatabaseCache) ShouldSync() bool {
	return time.Since(c.lastSync) > time.Duration(c.ttlMinutes)*time.Minute
}

// Cleanup removes the local cache file
func (c *DatabaseCache) Cleanup() error {
	if err := os.Remove(c.localPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
