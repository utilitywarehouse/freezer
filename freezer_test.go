package freezer

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/utilitywarehouse/straw"
)

func TestSinkCreatesNewDir(t *testing.T) {
	assert := assert.New(t)

	ss := straw.NewMemStreamStore()

	sink, err := NewMessageSink(ss, MessageSinkConfig{Path: "/foo/bar/baz"})
	if err != nil {
		t.Fatal(err)
	}
	assert.NoError(sink.Close())

	fi, err := ss.Stat("/foo/bar/baz")
	assert.NoError(err)
	assert.True(fi.IsDir())
	assert.Equal("baz", fi.Name())
}

func TestSimpleHappyPathRoundTrip(t *testing.T) {
	assert := assert.New(t)

	ss := straw.NewMemStreamStore()

	sink, err := NewMessageSink(ss, MessageSinkConfig{Path: "/foo/bar/baz"})
	if err != nil {
		t.Fatal(err)
	}

	assert.NoError(sink.PutMessage([]byte{1, 2, 3, 4, 5}))
	assert.NoError(sink.Close())

	source := NewMessageSource(ss, MessageSourceConfig{Path: "/foo/bar/baz"})

	messages := make(chan []byte)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*1)
	defer cancel()
	go func() {
		source.ConsumeMessages(ctx, func(m []byte) error { messages <- m; return nil })
	}()

	select {
	case <-ctx.Done():
		t.Error("timeout before message")
	case m := <-messages:
		assert.Equal([]byte{1, 2, 3, 4, 5}, m)
	}

}

func TestMaxUnflushedTime(t *testing.T) {
	assert := assert.New(t)
	require := require.New(t)

	ss := straw.NewMemStreamStore()

	sink, err := NewMessageSink(ss, MessageSinkConfig{Path: "/foo/", MaxUnflushedTime: 5 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}

	assert.NoError(sink.PutMessage([]byte{1}))
	time.Sleep(7 * time.Millisecond)
	assert.NoError(sink.PutMessage([]byte{2}))
	assert.NoError(sink.Close())

	fis, err := ss.Readdir("/foo/00/00/00/00/00/00/")
	require.NoError(err)

	assert.Equal(2, len(fis))
	assert.Equal("00", fis[0].Name())
	assert.Equal("01", fis[1].Name())
}

func TestMaxUnflushedMessages(t *testing.T) {
	assert := assert.New(t)
	require := require.New(t)

	ss := straw.NewMemStreamStore()

	sink, err := NewMessageSink(ss, MessageSinkConfig{Path: "/foo/", MaxUnflushedTime: 5 * time.Second, MaxUnflushedMessages: 1})
	if err != nil {
		t.Fatal(err)
	}

	assert.NoError(sink.PutMessage([]byte{1}))
	assert.NoError(sink.PutMessage([]byte{2}))
	assert.NoError(sink.Close())

	fis, err := ss.Readdir("/foo/00/00/00/00/00/00/")
	require.NoError(err)

	assert.Equal(2, len(fis))
	assert.Equal("00", fis[0].Name())
	assert.Equal("01", fis[1].Name())
}

func TestAwaitInputFile(t *testing.T) {
	assert := assert.New(t)

	ss := straw.NewMemStreamStore()

	source := NewMessageSource(ss, MessageSourceConfig{Path: "/", PollPeriod: 20 * time.Millisecond})

	messages := make(chan []byte, 1)
	consumeErrors := make(chan error, 1)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*1)
	defer cancel()
	go func() {
		consumeErrors <- source.ConsumeMessages(ctx, func(m []byte) error { messages <- m; return nil })
	}()

	time.Sleep(30 * time.Millisecond)
	sink, err := NewMessageSink(ss, MessageSinkConfig{Path: "/", MaxUnflushedTime: 5 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}

	assert.NoError(sink.PutMessage([]byte{1, 2, 3, 4, 5}))

	select {
	case <-ctx.Done():
		t.Error("timeout before message")
	case m := <-messages:
		cancel()
		assert.Equal([]byte{1, 2, 3, 4, 5}, m)
		assert.NoError(sink.Close())
	}

	if err := <-consumeErrors; err != nil && err != ctx.Err() {
		t.Errorf("error during consume %v", err)
	}

}

func TestSourceContextCancelDuringRead(t *testing.T) {

	assert := assert.New(t)

	ss := straw.NewMemStreamStore()

	sink, err := NewMessageSink(ss, MessageSinkConfig{Path: "/foo/bar/baz"})
	if err != nil {
		t.Fatal(err)
	}

	assert.NoError(sink.PutMessage([]byte{1}))
	assert.NoError(sink.PutMessage([]byte{2}))
	assert.NoError(sink.Close())

	source := NewMessageSource(ss, MessageSourceConfig{Path: "/foo/bar/baz"})

	messages := make(chan []byte)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*1)
	consumeErr := make(chan error, 1)
	defer cancel()
	go func() {
		consumeErr <- source.ConsumeMessages(ctx, func(m []byte) error {
			select {
			case messages <- m:
				return nil
			case <-ctx.Done():
				return nil
			}
		})
	}()

	select {
	case <-ctx.Done():
		t.Error("timeout before message")
	case m := <-messages:
		assert.Equal([]byte{1}, m)
		cancel()
	}

	if err := <-consumeErr; err != nil {
		t.Error(err)
	}

}
