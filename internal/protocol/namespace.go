package protocol

// TLV napespaces
const (
	CustomNamespace uint8 = iota
	RouteNamespace
)

func WithNamespace(namespace uint8, id uint8) uint16 {
	return uint16(namespace)<<8 | uint16(id)
}

// Return namespace and id of a given key.
func Split(n uint16) (uint8, uint8) {
	namespace := uint8(n >> 8)
	id := uint8(n & 0xFF)
	return namespace, id
}
