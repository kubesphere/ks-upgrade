package storage

type Options struct {
	S3Options          *S3StorageOptions        `json:"s3"`
	FileStorageOptions *LocalFileStorageOptions `json:"local"`
}

func NewStorage(options *Options) (Storage, error) {
	if options.S3Options != nil {
		return NewS3Storage(options.S3Options)
	}
	return NewLocalFileStorage(options.FileStorageOptions)
}
