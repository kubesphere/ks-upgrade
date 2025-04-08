package storage

type Storage interface {
	Put(k string, object []byte) error
	Get(k string) ([]byte, error)
	Delete(k string) error
}
