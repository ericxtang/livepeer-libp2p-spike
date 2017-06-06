package core

import (
	"testing"

	crypto "github.com/libp2p/go-libp2p-crypto"
	peer "github.com/libp2p/go-libp2p-peer"
)

func TestStreamIDs(t *testing.T) {
	sid := RandomStreamID()
	_, pub, _ := crypto.GenerateKeyPair(crypto.RSA, 2048)
	pid, _ := peer.IDFromPublicKey(pub)

	strmID := MakeStreamID(pid, sid)

	newPid, newSid := strmID.SplitComponents()

	if newPid != pid || newSid != sid {
		t.Errorf("streamID: %v", strmID)
		t.Errorf("pid: %v, newPID: %v", pid, newPid)
		t.Errorf("sid: %v, newSid: %v", sid, newSid)
	}
}
