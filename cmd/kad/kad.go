package main

import (
	"context"
	"encoding/json"
	"flag"
	"io/ioutil"
	"path"
	"time"

	"github.com/ericxtang/livepeer-libp2p-spike/core"
	"github.com/golang/glog"
	crypto "github.com/libp2p/go-libp2p-crypto"
	net "github.com/libp2p/go-libp2p-net"
	peer "github.com/libp2p/go-libp2p-peer"
	ps "github.com/libp2p/go-libp2p-peerstore"
	protocol "github.com/libp2p/go-libp2p-protocol"
	ma "github.com/multiformats/go-multiaddr"
)

var LivepeerProtocol = protocol.ID("/livepeer")
var Peers []*core.LivepeerNode

type KeyFile struct {
	Pub  string
	Priv string
}

func getKeys(datadir string) (crypto.PrivKey, crypto.PubKey) {
	f, e := ioutil.ReadFile(path.Join(datadir, "keys.json"))
	var keyf KeyFile
	json.Unmarshal(f, &keyf)

	if e != nil || keyf.Priv == "" || keyf.Pub == "" {
		glog.Errorf("Cannot file keys in data dir, creating new keys")
		priv, pub, _ := crypto.GenerateKeyPair(crypto.RSA, 2048)

		privb, _ := priv.Bytes()
		pubb, _ := pub.Bytes()

		kf := KeyFile{Priv: crypto.ConfigEncodeKey(privb), Pub: crypto.ConfigEncodeKey(pubb)}
		kfb, _ := json.Marshal(kf)
		ioutil.WriteFile(path.Join(datadir, "keys.json"), kfb, 0644)

		return priv, pub
	}

	privb, _ := crypto.ConfigDecodeKey(keyf.Priv)
	pubb, _ := crypto.ConfigDecodeKey(keyf.Pub)
	priv, _ := crypto.UnmarshalPrivateKey(privb)
	pub, _ := crypto.UnmarshalPublicKey(pubb)
	return priv, pub
}

func handleProtocol(n *core.LivepeerNode, ws *core.WrappedStream) {
	var msg core.Message

	err := ws.Dec.Decode(&msg)
	if err != nil {
		glog.Errorf("Got error decoding msg: %v", err)
		return
	}
	//Protocol:
	//	- Join Swarm (and form kademlia network)
	//	- Get Peer Info???
	//	- Have(strmID, chunkID)
	//	- Request Video Chunk(strmID, chunkID) (ChunkDATA)
	//	- Transcode(strmID, config) (newStrmID)
	switch msg.Msg {
	case core.JoinMsgID:
		handleJoinSwarm(n, msg)

	case core.JoinAckID:
		glog.Infof("Recieved Ack")

	default:
	}

}

func handleJoinSwarm(n *core.LivepeerNode, msg core.Message) {
	//Join the swarm
	joinMsg, ok := msg.Data.(map[string]interface{})
	if !ok {
		glog.Errorf("Error converting JoinMsg: %v", msg.Data)
	}
	pid, _ := peer.IDB58Decode(joinMsg["ID"].(string))
	addr, _ := ma.NewMultiaddr(joinMsg["Addr"].(string))
	n.Peerstore.AddAddr(pid, addr, ps.PermanentAddrTTL)
	n.PeerHost.Connect(context.Background(), ps.PeerInfo{ID: pid})

	//Print current peers
	glog.Infof("%v Got join request from %v", n.Identity.Pretty(), pid.Pretty())
	glog.Infof("Current peers:\n")
	for _, p := range n.Peerstore.Peers() {
		glog.Infof("%v :: %v", p.Pretty(), n.Peerstore.Addrs(p))
	}
	glog.Infof("\n\n")

	//Send Ack
	n.SendJoinAck(pid)
}

func CreateNode(datadir string, port int, seedPeerID string, seedAddr string) {
	var priv crypto.PrivKey
	var pub crypto.PubKey
	if datadir == "" {
		priv, pub, _ = crypto.GenerateKeyPair(crypto.RSA, 2048)
	} else {
		priv, pub = getKeys(datadir)
	}

	n, err := core.NewNode(port, priv, pub)
	if err != nil {
		glog.Errorf("Cannot create node: %v", err)
		return
	}

	Peers = append(Peers, n)
	n.PeerHost.SetStreamHandler(LivepeerProtocol, func(s net.Stream) {
		wrappedStream := core.WrapStream(s)
		defer s.Close()
		handleProtocol(n, wrappedStream)
	})

	// glog.Infof("Node ID: %v", n.PeerHost.ID())
	// glog.Infof("Identity: %s", n.Identity.Pretty())
	// glog.Infof("peer.ID: %s", peer.ID("0x1442ef0"))
	// glog.Infof("Addrs: %v", n.Peerstore.Addrs())

	if seedPeerID != "" && seedAddr != "" {
		seedID, _ := peer.IDB58Decode(seedPeerID)
		addr, err := ma.NewMultiaddr(seedAddr)
		if err != nil {
			glog.Fatalf("Cannot join swarm: %v", err)
		}
		n.Peerstore.AddAddr(seedID, addr, ps.PermanentAddrTTL)
		n.PeerHost.Connect(context.Background(), ps.PeerInfo{ID: seedID})

		n.SendJoin(seedID)
	}
	glog.Infof("%v listening for connections on %v", n.Identity.Pretty(), n.PeerHost.Addrs())
	select {} // hang forever
}

func ConnectNode(n *core.LivepeerNode, seedPeerID, seedAddr string) {
	//Try and connect to the seed node
}

func main() {
	// glog.Infof("Starting node...")
	port := flag.Int("p", 0, "port")
	flag.Parse()

	if *port == 0 {
		glog.Fatalf("Please provide port")
	}

	//Initialize the Peer slice
	Peers = make([]*core.LivepeerNode, 0, 100)

	//Create Seed Node
	go CreateNode("", *port, "", "")

	time.Sleep(1 * time.Second)
	//Layer 1 peers, connect to the seed node
	newSeedPid := Peers[0].Identity.Pretty()
	newSeedAddr := Peers[0].PeerHost.Addrs()[0].String()
	glog.Infof("\n\n\n\n\nCreating 10 peers for seed: %v @ %v", newSeedPid, newSeedAddr)
	time.Sleep(1 * time.Second)
	for i := 0; i < 10; i++ {
		go CreateNode("", 10001+i, newSeedPid, newSeedAddr)
		time.Sleep(500 * time.Millisecond)
	}

	//Layer 2 peers, connect to the new seed node (the last node created in the previous loop)
	time.Sleep(5 * time.Second)
	newSeedPid = peer.IDB58Encode(Peers[5].Identity)
	newSeedAddr = Peers[5].PeerHost.Addrs()[0].String()
	glog.Infof("\n\n\n\n\nCreating 10 peers for seed: %v @ %v", newSeedPid, newSeedAddr)
	time.Sleep(1 * time.Second)
	for i := 0; i < 10; i++ {
		// glog.Infof("pid: %v, addr: %v", pid, addr.String())
		go CreateNode("", 10011+i, newSeedPid, newSeedAddr)
		time.Sleep(500 * time.Millisecond)
	}

	//Search for a peer
	time.Sleep(5 * time.Second)
	glog.Info("\n\n\n\n\nDo a peer search (between nodes NOT directly connected with each other)")
	time.Sleep(1 * time.Second)
	info, err := Peers[15].Routing.FindPeer(context.Background(), Peers[1].Identity)
	if err != nil {
		glog.Errorf("Error finding peer: %v", err)
	}
	glog.Infof("%v is looking for %v, found %v(%v)\n\nSuccess!!!", Peers[15].Identity.Pretty(), Peers[1].Identity.Pretty(), info.ID.Pretty(), info)

	select {} // hang forever
}
