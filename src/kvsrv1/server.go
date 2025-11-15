package kvsrv

import (
	"log"
	"sync"

	"6.5840/kvsrv1/rpc"
	"6.5840/labrpc"
	"6.5840/tester1"
)

const Debug = false

func DPrintf(format string, a ...interface{}) (n int, err error) {
	if Debug {
		log.Printf(format, a...)
	}
	return
}

type KVServer struct {
	mu sync.Mutex

	// Your definitions here.
	store   map[string]string
	version map[string]rpc.Tversion
}

func MakeKVServer() *KVServer {
	kv := &KVServer{
		store:   make(map[string]string),
		version: make(map[string]rpc.Tversion),
	}
	return kv
}

// Get returns the value and version for args.Key, if args.Key
// exists. Otherwise, Get returns ErrNoKey.
func (kv *KVServer) Get(args *rpc.GetArgs, reply *rpc.GetReply) {
	kv.mu.Lock()
	defer kv.mu.Unlock()
	val, ok := kv.store[args.Key]
	if !ok {
		reply.Err = rpc.ErrNoKey
		reply.Value = ""
		reply.Version = 0
		return
	}
	reply.Value = val
	reply.Version = kv.version[args.Key]
	reply.Err = rpc.OK
}

// Update the value for a key if args.Version matches the version of
// the key on the server. If versions don't match, return ErrVersion.
// If the key doesn't exist, Put installs the value if the
// args.Version is 0, and returns ErrNoKey otherwise.
func (kv *KVServer) Put(args *rpc.PutArgs, reply *rpc.PutReply) {
	kv.mu.Lock()
	defer kv.mu.Unlock()
	curVer, ok := kv.version[args.Key]
	if !ok {
		// key not present
		if args.Version == 0 { // create
			kv.store[args.Key] = args.Value
			kv.version[args.Key] = 1
			reply.Err = rpc.OK
		} else {
			reply.Err = rpc.ErrNoKey
		}
		return
	}
	// key exists
	if args.Version != curVer {
		reply.Err = rpc.ErrVersion
		return
	}
	kv.store[args.Key] = args.Value
	kv.version[args.Key] = curVer + 1
	reply.Err = rpc.OK
}

// You can ignore Kill() for this lab
func (kv *KVServer) Kill() {
}


// You can ignore all arguments; they are for replicated KVservers
func StartKVServer(ends []*labrpc.ClientEnd, gid tester.Tgid, srv int, persister *tester.Persister) []tester.IService {
	kv := MakeKVServer()
	return []tester.IService{kv}
}
