package logs

import (
	"io"
	"os"
)

// LogStreamer defines the interface for streaming detailed logs
type LogStreamer interface {
	io.Writer
	Close() error
}

// ConsoleStreamer simple streamer that writes to stdout
type ConsoleStreamer struct{}

func (c *ConsoleStreamer) Write(p []byte) (n int, err error) {
	return os.Stdout.Write(p)
}

func (c *ConsoleStreamer) Close() error {
	return nil
}

// MultiStreamer writes to a LogStreamer and any other io.Writers
type MultiStreamer struct {
	io.Writer
	streamer LogStreamer
}

func NewMultiStreamer(streamer LogStreamer, writers ...io.Writer) *MultiStreamer {
	allWriters := make([]io.Writer, 0, len(writers)+1)
	allWriters = append(allWriters, streamer)
	allWriters = append(allWriters, writers...)
	return &MultiStreamer{
		Writer:   io.MultiWriter(allWriters...),
		streamer: streamer,
	}
}

func (m *MultiStreamer) Close() error {
	return m.streamer.Close()
}
