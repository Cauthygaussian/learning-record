package kvraft

import (
	//"bytes"
	"sync"
	"sync/atomic"

	"6.5840/kvraft1/rsm"
	"6.5840/kvsrv1/rpc"
	"6.5840/labgob"
	"6.5840/labrpc"
	"6.5840/tester1"
)

type KVPair struct{
	Value string 
	Version rpc.Tversion
}

type KVServer struct {
	me   int
	dead int32 // set by Kill()
	rsm  *rsm.RSM

	// Your definitions here.
	mu  sync.Mutex 
	store map[string]KVPair //key-value存储
	locks map[string]*sync.RWMutex //key对应的锁
}

//获取特定键值的锁
func (kv *KVServer) getLock(key string) *sync.RWMutex{
	kv.mu.Lock()
	defer kv.mu.Unlock()

	_, exists := kv.locks[key]
	if !exists{
		kv.locks[key] = &sync.RWMutex{}
	}
	return kv.locks[key]
}

// To type-cast req to the right type, take a look at Go's type switches or type
// assertions below:
//
// https://go.dev/tour/methods/16
// https://go.dev/tour/methods/15
func (kv *KVServer) DoOp(req any) any {
	// Your code here
	switch r := req.(type){
	case *rpc.PutArgs:
		return kv.doPut(r)
	case *rpc.GetArgs:
		return kv.doGet(r)
	case rpc.PutArgs:
		return kv.doPut(&r)
	case rpc.GetArgs:
		return kv.doGet(&r)
	default:
		panic("unknown operation")
	}
	return nil
}

func (kv *KVServer) Snapshot() []byte {
	// Your code here
	return nil
}

func (kv *KVServer) Restore(data []byte) {
	// Your code here
}

func (kv *KVServer) doGet(args *rpc.GetArgs) *rpc.GetReply {
	if kv.killed(){
		return &rpc.GetReply{Err: rpc.ErrWrongLeader}
	}

	keyLock := kv.getLock(args.Key)
	keyLock.RLock()
	defer keyLock.RUnlock()

	pair, ok := kv.store[args.Key]
	if ok{
		return &rpc.GetReply{
			Err: rpc.OK,
			Value: pair.Value,
			Version: pair.Version,
		}
	}else{
		return &rpc.GetReply{
			Err: rpc.ErrNoKey,
		}
	}
}

func (kv *KVServer) doPut(args *rpc.PutArgs) *rpc.PutReply{
	if kv.killed(){
		return &rpc.PutReply{
			Err: rpc.ErrWrongLeader,
		}
	}

	keyLock := kv.getLock(args.Key)
	keyLock.Lock()
	defer keyLock.Unlock()

	pair, ok := kv.store[args.Key]
	if ok{
		if args.Version == pair.Version{
			kv.store[args.Key] = KVPair{
				Value: args.Value,
				Version: pair.Version + 1,
			}
			return &rpc.PutReply{
				Err: rpc.OK,
			}
		}else{
			return &rpc.PutReply{
				Err: rpc.ErrVersion,
			}
		}
	}else{
		if args.Version == 0{
			kv.store[args.Key] = KVPair{
				Value: args.Value,
				Version: 1,
			}
			return &rpc.PutReply{
				Err: rpc.OK,
			}
		}else{
			return &rpc.PutReply{
				Err: rpc.ErrVersion,
			}
		}
	}
}

func (kv *KVServer) Get(args *rpc.GetArgs, reply *rpc.GetReply) {
	// Your code here. Use kv.rsm.Submit() to submit args
	// You can use go's type casts to turn the any return value
	// of Submit() into a GetReply: rep.(rpc.GetReply)
	if kv.killed(){
		reply.Err = rpc.ErrWrongLeader
		return
	}

	err, rep := kv.rsm.Submit(args)
	if err == rpc.ErrWrongLeader{
		reply.Err = rpc.ErrWrongLeader
		return 
	}
	getReply , ok := rep.(*rpc.GetReply)
	if !ok{
		reply.Err = rpc.ErrWrongLeader
		return 
	}
	*reply = *getReply
}

func (kv *KVServer) Put(args *rpc.PutArgs, reply *rpc.PutReply) {
	// Your code here. Use kv.rsm.Submit() to submit args
	// You can use go's type casts to turn the any return value
	// of Submit() into a PutReply: rep.(rpc.PutReply)
	if kv.killed(){
		reply.Err = rpc.ErrWrongLeader
		return
	}

	err, rep := kv.rsm.Submit(args)
	if err == rpc.ErrWrongLeader{
		reply.Err = rpc.ErrWrongLeader
		return 
	}
	PutReply , ok := rep.(*rpc.PutReply)
	if !ok{
		reply.Err = rpc.ErrWrongLeader
		return 
	}
	*reply = *PutReply
}

// the tester calls Kill() when a KVServer instance won't
// be needed again. for your convenience, we supply
// code to set rf.dead (without needing a lock),
// and a killed() method to test rf.dead in
// long-running loops. you can also add your own
// code to Kill(). you're not required to do anything
// about this, but it may be convenient (for example)
// to suppress debug output from a Kill()ed instance.
func (kv *KVServer) Kill() {
	atomic.StoreInt32(&kv.dead, 1)
	// Your code here, if desired.
}

func (kv *KVServer) killed() bool {
	z := atomic.LoadInt32(&kv.dead)
	return z == 1
}

// StartKVServer() and MakeRSM() must return quickly, so they should
// start goroutines for any long-running work.
func StartKVServer(servers []*labrpc.ClientEnd, gid tester.Tgid, me int, persister *tester.Persister, maxraftstate int) []tester.IService {
	// call labgob.Register on structures you want
	// Go's RPC library to marshall/unmarshall.
	labgob.Register(rsm.Op{})
	labgob.Register(rpc.PutArgs{})
	labgob.Register(rpc.GetArgs{})
	labgob.Register(KVPair{})
	labgob.Register(map[string]KVPair{})

	kv := &KVServer{
		me: me,
		mu: sync.Mutex{},
		store: make(map[string]KVPair),
		locks: make(map[string]*sync.RWMutex),
	}


	kv.rsm = rsm.MakeRSM(servers, me, persister, maxraftstate, kv)
	// You may need initialization code here.
	return []tester.IService{kv, kv.rsm.Raft()}
}
