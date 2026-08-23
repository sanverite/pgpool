package protocol_test

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/sanverite/pgpool/internal/protocol"
)

// buildStartupMessage constructs a raw startup message byte slice.
// This is what psql actually sends on the wire.
func buildStartupMessage(version uint32, params map[string]string) []byte {
	var body bytes.Buffer

	// Write protocol version — 4 bytes big-endian.
	versionBytes := make([]byte, 4)
	binary.BigEndian.PutUint32(versionBytes, version)
	body.Write(versionBytes)

	// Write key-value pairs as null-terminated strings.
	for k, v := range params {
		body.WriteString(k)
		body.WriteByte(0)
		body.WriteString(v)
		body.WriteByte(0)
	}

	// Write the final null byte that terminates the parameter list.
	body.WriteByte(0)

	// Prepend the length — 4 bytes for length itself + body length.
	totalLength := uint32(4 + body.Len())
	lengthBytes := make([]byte, 4)
	binary.BigEndian.PutUint32(lengthBytes, totalLength)

	var msg bytes.Buffer
	msg.Write(lengthBytes)
	msg.Write(body.Bytes())

	return msg.Bytes()
}

func TestReadStartupMessage(t *testing.T) {
	// Protocol version 3.0 — what every modern Postgres client sends.
	// Encoded as (3 << 16 | 0) = 196608.
	const protocolVersion = uint32(3<<16 | 0)

	params := map[string]string{
		"user":             "postgres",
		"database":         "mydb",
		"application_name": "psql",
	}

	raw := buildStartupMessage(protocolVersion, params)

	// bytes.NewReader implements io.Reader — lets us test
	// without a real network connection.
	buf := bytes.NewBuffer(raw)
	msg, err := protocol.ReadStartupMessage(buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if msg.ProtocolVersion != protocolVersion {
		t.Errorf("expected version %d, got %d", protocolVersion, msg.ProtocolVersion)
	}

	if msg.Parameters["user"] != "postgres" {
		t.Errorf("expected user 'postgres', got %q", msg.Parameters["user"])
	}

	if msg.Parameters["database"] != "mydb" {
		t.Errorf("expected database 'mydb', got %q", msg.Parameters["database"])
	}
}

func TestReadStartupMessageTooShort(t *testing.T) {
	// A message with only 3 bytes — too short to contain a valid length.
	raw := []byte{0, 0, 0}

	buf := bytes.NewBuffer(raw)
	_, err := protocol.ReadStartupMessage(buf)
	if err == nil {
		t.Fatal("expected error for truncated message, got nil")
	}
}

func TestReadStartupMessageInvalidLength(t *testing.T) {
	// A message that claims to be 2 bytes long — invalid since
	// the length field itself is 4 bytes.
	raw := make([]byte, 4)
	binary.BigEndian.PutUint32(raw, 2) // length of 2 is impossible

	_, err := protocol.ReadStartupMessage(bytes.NewBuffer(raw))
	if err == nil {
		t.Fatal("expected error for invalid length, got nil")
	}
}
