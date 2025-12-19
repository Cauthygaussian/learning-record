package rsm

import (
	//"bytes"
	"sync"
	"sync/atomic"
	"time"

	"6.5840/kvsrv1/rpc"
	//"6.5840/labgob"
	"6.5840/labrpc"
	"6.5840/raft1"
	"6.5840/raftapi"
	"6.5840/tester1"
)

var useRaftStateMachine bool // to plug in another raft besided raft1


type Op struct {
	// Your definitions here.
	// Field names must start with capital letters,
	// otherwise RPC will break.
	Me int 
	ID int64 
	Req any 
}


// A server (i.e., ../server.go) that wants to replicate itself calls
// MakeRSM and must implement the StateMachine interface.  This
// interface allows the rsm package to interact with the server for
// server-specific operations: the server must implement DoOp to
// execute an operation (e.g., a Get or Put request), and
// Snapshot/Restore to snapshot and restore the server's state.
type StateMachine interface {
	DoOp(any) any
	Snapshot() []byte
	Restore([]byte)
}

type RSM struct {
	mu           sync.Mutex
	me           int
	rf           raftapi.Raft
	applyCh      chan raftapi.ApplyMsg
	maxraftstate int // snapshot if log grows this big
	sm           StateMachine
	// Your definitions here.
	operationID int64  //生成唯一的operation ID
	pendingOps map[int]*PendingOp //存储没有完成的操作
	shutdown atomic.Bool  //标记是否关闭

}

type PendingOp struct{
	op Op  //操作
	result any //操作结果
	done chan bool //操作完成的信号通道
}

// servers[] contains the ports of the set of
// servers that will cooperate via Raft to
// form the fault-tolerant key/value service.
//
// me is the index of the current server in servers[].
//
// the k/v server should store snapshots through the underlying Raft
// implementation, which should call persister.SaveStateAndSnapshot() to
// atomically save the Raft state along with the snapshot.
// The RSM should snapshot when Raft's saved state exceeds maxraftstate bytes,
// in order to allow Raft to garbage-collect its log. if maxraftstate is -1,
// you don't need to snapshot.
//
// MakeRSM() must return quickly, so it should start goroutines for
// any long-running work.
func MakeRSM(servers []*labrpc.ClientEnd, me int, persister *tester.Persister, maxraftstate int, sm StateMachine) *RSM {
	rsm := &RSM{
		me:           me,
		maxraftstate: maxraftstate,
		applyCh:      make(chan raftapi.ApplyMsg),
		sm:           sm,
		operationID:  0, 
		pendingOps:   make(map[int]*PendingOp),
	}
	rsm.shutdown.Store(false)

	if !useRaftStateMachine {
		rsm.rf = raft.Make(servers, me, persister, rsm.applyCh)
	}

	go rsm.reader()

	return rsm
}

func (rsm *RSM) Raft() raftapi.Raft {
	return rsm.rf
}

// Kill is called by the tester when it's done with the RSM instance.
// It triggers immediate shutdown to unblock any waiting Submit() calls.
func (rsm *RSM) Kill() {
	// DPrintf("RSM %d Kill() called", rsm.me)
	if rsm.rf != nil {
		rsm.rf.Kill()
	}
	rsm.handleShutdown()
}


// Submit a command to Raft, and wait for it to be committed.  It
// should return ErrWrongLeader if client should find new leader and
// try again.
func (rsm *RSM) Submit(req any) (rpc.Err, any) {

	// Submit creates an Op structure to run a command through Raft;
	// for example: op := Op{Me: rsm.me, Id: id, Req: req}, where req
	// is the argument to Submit and id is a unique id for the op.

	// your code here
	if rsm.shutdown.Load(){
		return rpc.ErrWrongLeader, nil // i'm dead, try another server.
	}

	opID := atomic.AddInt64(&rsm.operationID, 1)
	op := Op{
		Me : rsm.me, 
		ID: opID, 
		Req: req ,
	}

	index, term , isLeader := rsm.rf.Start(op)
	if !isLeader{
		return rpc.ErrWrongLeader, nil
	}

	pendingOp := &PendingOp{
		op : op ,
		done : make(chan bool, 1) ,
	}

	rsm.mu.Lock()
	oldPendingOp, exists := rsm.pendingOps[index]
	if exists {
		select {
			case oldPendingOp.done <- false:
			default:
		}
	}
	rsm.pendingOps[index] = pendingOp
	
	// Check shutdown after adding to map to avoid race with Kill()
	if rsm.shutdown.Load() {
		delete(rsm.pendingOps, index)
		rsm.mu.Unlock()
		return rpc.ErrWrongLeader, nil
	}
	rsm.mu.Unlock()

	err, result := rsm.waitForResult(pendingOp, term)

	rsm.mu.Lock()
	delete(rsm.pendingOps, index)
	rsm.mu.Unlock()

	return err, result 
}

func (rsm *RSM) waitForResult(pendingOp *PendingOp, term int) (rpc.Err, any){
	// Check shutdown immediately to avoid race between Kill() and adding to pendingOps
	if rsm.shutdown.Load(){
		return rpc.ErrWrongLeader, nil
	}

	timeout := time.NewTimer(100 * time.Millisecond)
	defer timeout.Stop()

	// Periodic shutdown check ticker
	shutdownCheck := time.NewTicker(10 * time.Millisecond)
	defer shutdownCheck.Stop()

	for{
		select{
		case <- shutdownCheck.C:
			if rsm.shutdown.Load(){
				return rpc.ErrWrongLeader, nil
			}
		case <- timeout.C:
			if rsm.shutdown.Load(){
				return rpc.ErrWrongLeader, nil
			}
			currentTerm, isLeader := rsm.rf.GetState()
			if !isLeader || currentTerm != term{
				return rpc.ErrWrongLeader, nil
			}
			timeout.Reset(100 * time.Millisecond)
		case res := <- pendingOp.done:
			if res{
				return rpc.OK, pendingOp.result 
			}else{
				return rpc.ErrWrongLeader, nil 
			}
		}
	}
}

func (rsm *RSM) reader(){
	for{
		msg, ok := <-rsm.applyCh 
		if !ok{
			rsm.handleShutdown()
			return 
		}
		if rsm.shutdown.Load(){
			return 
		}
		if msg.CommandValid{
			rsm.applyCommand(msg)
		}
	}
}

func (rsm *RSM) handleShutdown(){
	rsm.mu.Lock()
	defer rsm.mu.Unlock()

	rsm.shutdown.Store(true)

	for _, pendingOp := range rsm.pendingOps{
		select{
		case pendingOp.done <- false:
		default: 
		}
	}

	rsm.pendingOps = make(map[int]*PendingOp)
}

func (rsm *RSM) applyCommand(msg raftapi.ApplyMsg){
	op, ok := msg.Command.(Op)

	if !ok{
		panic("RSM.applyCommand: cannot cast msg.Command to Op")
	}
	rsm.mu.Lock()

	result := rsm.sm.DoOp(op.Req)

	pendingOp, exists := rsm.pendingOps[msg.CommandIndex]

	if exists{
		if pendingOp.op.ID == op.ID && pendingOp.op.Me == rsm.me{
			pendingOp.result = result
			select{
			case pendingOp.done <- true:
			default: 
			}
		}else{
			select{
			case pendingOp.done <- false:
			default:
			}
		}
	}

	rsm.mu.Unlock()
}
