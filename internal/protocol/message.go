package protocol

import (
	"encoding/binary"
	"fmt"
	"io"
)

// MaxMessageSize is the maximum allowed message size.
// Prevents memory exhaustion from malformed or malicious clients.
// 10MB is generous - real startup messages are under 10KB.
const MaxMessageSize = 10 * 1024 * 1024 // 10MB

// StartupMessage is the first message a Postgres client sends.
// It contains the protocol version and connection parameters
// like username, database, and application name.
type StartupMessage struct {
	// ProtocolVersion is a 32-bit integer encoding major and minor
	// version. Postgres 3.0 encodes as (3 << 16 | 0) = 196608.
	ProtocolVersion uint32

	// Parameters holds key-value pairs sent by the client.
	// Common keys: "user", "database", "application_name".
	Parameters map[string]string
}

// SSLRequestCode is the protocol version sent by clients
// when they want to negotiate an SSL connection.
// Value: (1234 << 16 | 5679) = 80877103
const SSLRequestCode = 80877103

// SSLResponse is the single byte we send back to decline SSL.
// 'N' means "no SSL supported here, send your startup message".
// 'S' would mean "yes upgrade to SSL".
const SSLResponse = byte('N')

// ReadStartupMessage reads the startup message from r.
// It transparently handles SSL negotiation - if the client
// sends an SSL request first, we decline it and read the
// real startup message that follows.
func ReadStartupMessage(r io.ReadWriter) (*StartupMessage, error) {
	msg, err := readStartupMessage(r)
	if err != nil {
		return nil, err
	}

	// Client is asking for SSL. Decline with 'N' and read
	// the real startup message that follows.
	if msg.ProtocolVersion == SSLRequestCode {
		if _, err := r.Write([]byte{SSLResponse}); err != nil {
			return nil, fmt.Errorf("sending SSL denial: %w", err)
		}

		// Read the real startup message now
		return readStartupMessage(r)
	}

	return msg, nil
}

// readStartupMessage reads and parses the startup message from r.
//
// The startup message format is unique - unlike all other Postgres
// messages it has no leading type byte.
//
//	┌─────────────────────────────────────────┐
//	│ Length  │ 4 bytes │ total length         │
//	│ Version │ 4 bytes │ protocol version     │
//	│ Payload │ N bytes │ null-terminated pairs│
//	└─────────────────────────────────────────┘
//
// Length includes its own 4 bytes in the count.
func readStartupMessage(r io.Reader) (*StartupMessage, error) {
	// Step 1: Read the first 4 bytes to get the total message length.
	// We use a fixed-size array here - we know exactly how many
	// bytes we need, no allocation required.
	var lengthBuf [4]byte
	if _, err := io.ReadFull(r, lengthBuf[:]); err != nil {
		return nil, fmt.Errorf("reading startup message length: %w", err)
	}

	// binary.BigEndian interprets bytes as big-endian integer.
	// Network protocols use big-endian by convention.
	// The length includes its own 4 bytes, so subtract them
	// to get the remaining bytes to read.
	totalLength := binary.BigEndian.Uint32(lengthBuf[:])
	if totalLength < 4 {
		return nil, fmt.Errorf("invalid startup message length: %d", totalLength)
	}

	remaining := totalLength - 4 // subtract the 4 bytes we already read

	if remaining > MaxMessageSize {
		return nil, fmt.Errorf("startup message too large: %d bytes", remaining)
	}

	// Step 2: read the rest of the message into a buffer.
	buf := make([]byte, remaining)
	if _, err := io.ReadFull(r, buf); err != nil {
		return nil, fmt.Errorf("reading startup message body: %w", err)
	}

	// Step 3: first 4 bytes of the body are the protocol version.
	if len(buf) < 4 {
		return nil, fmt.Errorf("startup message body too short: %d bytes", len(buf))
	}

	version := binary.BigEndian.Uint32(buf[:4])

	// Step 4: parse the key-value parameters.
	// Format: null-terminated strings, alternating key and value.
	// Terminated by a single null byte.
	//
	// Example: "user\0postgres\0database\0mydb\0\0"
	params, err := parseParameters(buf[4:])
	if err != nil {
		return nil, fmt.Errorf("parsing startup parameters: %w", err)
	}

	return &StartupMessage{
		ProtocolVersion: version,
		Parameters:      params,
	}, nil
}

// parseParameters parses null-terminated key-value pairs from b.
// The list is terminated by a single null byte.
func parseParameters(b []byte) (map[string]string, error) {
	params := make(map[string]string)

	for len(b) > 0 {
		// A single null byte marks the end of the parameter list.
		if b[0] == 0 {
			break
		}

		// Read key - everything up to next null byte.
		key, rest, err := readCString(b)
		if err != nil {
			return nil, fmt.Errorf("reading key paramenter: %w", err)
		}
		b = rest

		// Read value - everything up to the next null byte.
		value, rest, err := readCString(b)
		if err != nil {
			return nil, fmt.Errorf("reading value parameter: %w", err)
		}
		b = rest

		params[key] = value
	}

	return params, nil
}

// readCString reads a null-terminated string from b.
// Returns the string, the remaining bytes after the null, and any error.
//
// C strings are null-terminated - the string ends at the first 0 byte.
// This is the format Postgres uses for strings in its wire protocol.
func readCString(b []byte) (string, []byte, error) {
	for i, c := range b {
		if c == 0 {
			// Found the null terminator.
			// Return the string before it and bytes after it.
			return string(b[:i]), b[i+1:], nil
		}
	}

	return "", nil, fmt.Errorf("no null terminator found in string")
}
