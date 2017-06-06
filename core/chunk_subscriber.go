package core

import (
	"strings"

	"github.com/livepeer/lpms/stream"
)

type ChunkSubscriber struct {
	Buffer *stream.HLSBuffer
	Node   *LivepeerNode
}

func NewChunkSubscriber(buf *stream.HLSBuffer, n *LivepeerNode) *ChunkSubscriber {
	return &ChunkSubscriber{Buffer: buf, Node: n}
}

func (cs *ChunkSubscriber) WriteSegment(seqNo uint64, name string, duration float64, s []byte) error {
	//Send out Have Messages
	strmID := strings.Split(name, "_")[0]
	// cs.Node.SendHaveChunk(name, strmID, peer.IDHexEncode(cs.Node.Identity))
	cs.Node.SendHaveChunk(name, strmID)

	return cs.Buffer.WriteSegment(seqNo, name, duration, s)
}
