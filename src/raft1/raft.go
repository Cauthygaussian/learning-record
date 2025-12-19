package raft

// The file raftapi/raft.go defines the interface that raft must
// expose to servers (or the tester), but see comments below for each
// of these functions for more details.
//
// Make() creates a new raft peer that implements the raft interface.
// ApplyMsg
//   each time a new entry is committed to the log, each Raft peer
//   should send an ApplyMsg to the service (or tester)
//   in the same server.
//

import (
	"bytes"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"
	"fmt"

	"6.5840/labgob"
	"6.5840/labrpc"
	"6.5840/raftapi"
	"6.5840/tester1"
)

const (
	Follower = iota 
	Candidate 
	Leader 
)

type logEntry struct {
	Command interface{} // 客户端请求的命令
	Term    int         // 日志条目被添加时的任期
}

type AppendEntriesArgs struct { 
	Term         int
	LeaderID     int
	PrevLogIndex int        // 上一个日志索引
	PrevLogTerm  int        // 上一个日志任期
	LeaderCommit int        // 领导人已提交的最高日志索引
	Entries      []logEntry // 附带的日志条目
}

type AppendEntriesReply struct {
	Term    int
	Success bool // 表示随从包含的项目是否匹配上一个日志索引和上一个日志任期
	CommitIndex int //随从已经提交的日志号码
	ConflictTerm int // 冲突的 term（如果有）
	ConflictIndex int // follower 建议的回退 index
}


// A Go object implementing a single Raft peer.
type Raft struct {
	mu        sync.Mutex          // Lock to protect shared access to this peer's state
	peers     []*labrpc.ClientEnd // RPC end points of all peers
	persister *tester.Persister   // Object to hold this peer's persisted state
	me        int                 // this peer's index into peers[]
	dead      int32               // set by Kill()

	// Your data here (3A, 3B, 3C).
	// Look at the paper's Figure 2 for a description of what
	// state a Raft server must maintain.

	currentTerm int    //当前任期
	votedFor int       //投票给了谁
	log []logEntry      //当前的日志条目


	//servers的易失性状态
	commitIndex int  //已提交的最高日志条目的索引
	lastApplied int  //已应用到状态机的最高日志条目的索引

	//领导人的易失性状态
	nextIndex []int   //对于每个服务器，要发送到该服务器的下一个日志条目的索引
	matchIndex []int  //对于每个服务器，已知已复制到该服务器的最高日志条目的索引

	//自己写的
	state int    //当前节点的状态：跟随者、候选人、领导人
	electionTimer *time.Timer //选举定时器
	heartbeatTimer *time.Timer //心跳定时器
	lastHeartbeat time.Time       // 最近一次收到心跳/授票时间
	applyCh chan raftapi.ApplyMsg   //用于发送消息的通道
	applyCond *sync.Cond    
	replicatorCond []*sync.Cond 

	// 快照相关lab3D
	lastIncludedIndex int
	lastIncludedTerm int

}

type InstallSnapshotArgs struct{
	Term                  int 
	LeaderID              int 
	LastIncludedIndex     int
	LastIncludedTerm      int 
	Offset                int 
	Data                  []byte 
	Done                  bool 
}

type InstallSnapshotReply struct{
	Term int 
}

// example RequestVote RPC arguments structure.
// field names must start with capital letters!
type RequestVoteArgs struct {
	// exported fields for gob/RPC
	Term         int // 候选人的任期
	CandidateID  int // 候选人的ID
	LastLogIndex int // 上一个日志的索引
	LastLogTerm  int // 上一个日志的任期
}

// example RequestVote RPC reply structure.
// field names must start with capital letters!
type RequestVoteReply struct {
	Term        int  // 当前任期，方便更新
	VoteGranted bool // 是否授予选票
} 


// return currentTerm and whether this server
// believes it is the leader.
func (rf *Raft) GetState() (int, bool) {
	rf.mu.Lock()
	defer rf.mu.Unlock()
	var term int
	var isleader bool
	// Your code here (3A).
	term = rf.currentTerm 
	// After Kill(), return false for isleader so higher-level services stop waiting
	isleader = (rf.state == Leader) && !rf.killed()
	return term, isleader
}

func randomElectionTimeout() time.Duration{
	//150ms - 300ms
	return time.Duration(150 + rand.Int63() % 150) * time.Millisecond 
}

func heartbeatTimeout() time.Duration{
	//50ms
	return 50 * time.Millisecond 
}


//this function used to state change and adjustment
func (rf *Raft) ChangeState(statement int) {
    if rf.state == statement {
        return
    }
    rf.state = statement
    switch statement {
    case Leader:
        if rf.heartbeatTimer != nil {
            rf.heartbeatTimer.Reset(heartbeatTimeout())
        }
        if rf.electionTimer != nil {
            rf.electionTimer.Stop()
        }
        
        // ---------------- 关键修正 ----------------
        // 变为 Leader 时，将所有 Follower 的 nextIndex 初始化为 Leader 的 (LastIndex + 1)
        // 这里的 LastIndex 必须是绝对索引！
        lastIdx := rf.getLastIndex()
        for i := 0; i < len(rf.peers); i++ {
            rf.nextIndex[i] = lastIdx + 1
            rf.matchIndex[i] = 0 // 保守起见，初始化为 0
        }
        rf.matchIndex[rf.me] = lastIdx
        // ----------------------------------------
        
        rf.SendHeartBeats()

    case Follower, Candidate:
        if rf.heartbeatTimer != nil {
            rf.heartbeatTimer.Stop()
        }
        if rf.electionTimer != nil {
            rf.electionTimer.Reset(randomElectionTimeout())
        }
    }
}


// save Raft's persistent state to stable storage,
// where it can later be retrieved after a crash and restart.
// see paper's Figure 2 for a description of what should be persistent.
// before you've implemented snapshots, you should pass nil as the
// second argument to persister.Save().
// after you've implemented snapshots, pass the current snapshot
// (or nil if there's not yet a snapshot).
func (rf *Raft) persist() {
    w := new(bytes.Buffer)
    e := labgob.NewEncoder(w)
    // 必须编码所有持久化状态
    e.Encode(rf.currentTerm)
    e.Encode(rf.votedFor)
    e.Encode(rf.log)
    // ---------------- 新增下面两行 ----------------
    e.Encode(rf.lastIncludedIndex)
    e.Encode(rf.lastIncludedTerm)
    // -------------------------------------------
    raftstate := w.Bytes()
    rf.persister.Save(raftstate, rf.persister.ReadSnapshot())
}


// restore previously persisted state.
func (rf *Raft) readPersist(data []byte) {
    if data == nil || len(data) < 1 { // bootstrap without any state?
        return
    }

    r := bytes.NewBuffer(data)
    d := labgob.NewDecoder(r)
    
    var currentTerm int 
    var votedFor int 
    var log []logEntry 
    // ---------------- 新增变量 ----------------
    var lastIncludedIndex int
    var lastIncludedTerm int
    // ----------------------------------------

    // 注意 Decode 的顺序必须和 Encode 一致
    if d.Decode(&currentTerm) != nil || 
       d.Decode(&votedFor) != nil || 
       d.Decode(&log) != nil ||
       d.Decode(&lastIncludedIndex) != nil || // 新增
       d.Decode(&lastIncludedTerm) != nil {   // 新增
        fmt.Println("decode error!")
    } else {
        rf.currentTerm = currentTerm
        rf.votedFor = votedFor 
        rf.log = log 
        // ---------------- 恢复状态 ----------------
        rf.lastIncludedIndex = lastIncludedIndex
        rf.lastIncludedTerm = lastIncludedTerm
        // ----------------------------------------
    }
}

// how many bytes in Raft's persisted log?
func (rf *Raft) PersistBytes() int {
	rf.mu.Lock()
	defer rf.mu.Unlock()
	return rf.persister.RaftStateSize()
}

func (rf *Raft) getLastIndex() int{
	return len(rf.log) + rf.lastIncludedIndex
}

func (rf *Raft) getLastTerm() int{
	if len(rf.log) > 0{
		return rf.log[len(rf.log) - 1].Term 
	}
	return rf.lastIncludedTerm
}

//获取某个真实index的term
func (rf *Raft) getIndexTerm(index int) int{
	if index == rf.lastIncludedIndex{
		return rf.lastIncludedTerm 
	}

	offset := index - rf.lastIncludedIndex - 1

	if offset < 0  || offset >= len(rf.log){
		return -1
	}

	return rf.log[offset].Term 
}

func (rf *Raft) getSlice(start int) []logEntry{
	return rf.log[start - rf.lastIncludedIndex - 1:]
}


// the service says it has created a snapshot that has
// all info up to and including index. this means the
// service no longer needs the log through (and including)
// that index. Raft should now trim its log as much as possible.
func (rf *Raft) Snapshot(index int, snapshot []byte) {
	// Your code here (3D).
	rf.mu.Lock()
	defer rf.mu.Unlock()

	// index 为逻辑索引：lastIncludedIndex < index <= commitIndex
	if index <= rf.lastIncludedIndex || index > rf.commitIndex {
		return 
	}

	// 计算在当前 log 中的偏移
	offset := index - rf.lastIncludedIndex - 1
	if offset < 0 || offset >= len(rf.log) {
		return
	}

	// 更新快照元数据
	rf.lastIncludedTerm = rf.log[offset].Term
	rf.lastIncludedIndex = index

	// 保留 index 之后的日志
	if offset+1 < len(rf.log) {
		rf.log = rf.log[offset+1:]
	} else {
		rf.log = nil
	}

	// 调整 commitIndex / lastApplied 至少不小于 lastIncludedIndex
	if rf.commitIndex < rf.lastIncludedIndex {
		rf.commitIndex = rf.lastIncludedIndex
	}
	if rf.lastApplied < rf.lastIncludedIndex {
		rf.lastApplied = rf.lastIncludedIndex
	}

	// 持久化状态与快照
	w := new(bytes.Buffer)
	e := labgob.NewEncoder(w)
	_ = e.Encode(rf.currentTerm)
	_ = e.Encode(rf.votedFor)
	_ = e.Encode(rf.log)
	_ = e.Encode(rf.lastIncludedIndex)
	_ = e.Encode(rf.lastIncludedTerm)
	raftstate := w.Bytes()
	rf.persister.Save(raftstate, snapshot)
	return 

}

//rf是接收快照的那个，也就是follower，args记载了安装快照的相关信息
func (rf *Raft) InstallSnapShot(args *InstallSnapshotArgs, reply *InstallSnapshotReply){
	rf.mu.Lock()

	if args.Term < rf.currentTerm{
		reply.Term = rf.currentTerm
		rf.mu.Unlock()
		return 
	}

	rf.ChangeState(Follower)

	// 如果收到的 snapshot 不比当前的新，直接忽略（但要回复，避免 leader 重传）
	if args.LastIncludedIndex <= rf.lastIncludedIndex {
		reply.Term = rf.currentTerm
		rf.mu.Unlock()
		return
	}

	// 计算需要丢弃的前缀长度（基于旧的 lastIncludedIndex）
	offset := args.LastIncludedIndex - rf.lastIncludedIndex
	if offset < len(rf.log) {
		rf.log = rf.log[offset:]
	} else {
		rf.log = nil
	}

	rf.lastIncludedIndex = args.LastIncludedIndex
	rf.lastIncludedTerm = args.LastIncludedTerm
	rf.commitIndex = max(rf.commitIndex, args.LastIncludedIndex)
	rf.lastApplied = max(rf.lastApplied, args.LastIncludedIndex)

	// 持久化新的状态和快照
	w := new(bytes.Buffer)
	e := labgob.NewEncoder(w)
	_ = e.Encode(rf.currentTerm)
	_ = e.Encode(rf.votedFor)
	_ = e.Encode(rf.log)
	_ = e.Encode(rf.lastIncludedIndex)
	_ = e.Encode(rf.lastIncludedTerm)
	raftstate := w.Bytes()
	rf.persister.Save(raftstate, args.Data)

	// 构造消息
    msg := raftapi.ApplyMsg{
        SnapshotValid: true,
        Snapshot: args.Data,
        SnapshotTerm: args.LastIncludedTerm,
        SnapshotIndex: args.LastIncludedIndex,
    }

	// 唤醒 applier，避免在 snapshot 后 commitIndex == lastApplied 导致死锁
	rf.applyCond.Broadcast()
	rf.mu.Unlock()

	rf.applyCh <- msg



}


func (rf *Raft) sendInstallSnapshot(server int, args *InstallSnapshotArgs, reply *InstallSnapshotReply) bool {
    ok := rf.peers[server].Call("Raft.InstallSnapShot", args, reply)
    return ok
}




// example RequestVote RPC handler.
//rf是投票的那个，args记载了请求投票的相关信息
func (rf *Raft) RequestVote(args *RequestVoteArgs, reply *RequestVoteReply) {
	// Your code here (3A, 3B).
	rf.mu.Lock()
	defer rf.mu.Unlock()
	reply.VoteGranted = false

	myLastTerm := rf.getLastTerm()
	myLastIndex := rf.getLastIndex()

	if args.Term < rf.currentTerm{
		reply.Term = rf.currentTerm 
		return 
	}

	if args.Term > rf.currentTerm{
		rf.currentTerm = args.Term 
		rf.votedFor = -1
		rf.ChangeState(Follower)
	}


	// 判断日志是否至少和自己一样新
	upToDate := (args.LastLogTerm > myLastTerm) || (args.LastLogTerm == myLastTerm && args.LastLogIndex >= myLastIndex)

	if (rf.votedFor == -1 || rf.votedFor == args.CandidateID) && upToDate{
		rf.votedFor = args.CandidateID 
		rf.lastHeartbeat = time.Now()
		rf.electionTimer.Reset(randomElectionTimeout())

		reply.VoteGranted = true
	}
	
	reply.Term = rf.currentTerm
	rf.persist()
	return
}

func (rf *Raft) AppendEntries(args *AppendEntriesArgs, reply *AppendEntriesReply){
	// fmt.Printf("[AE] me=%d BEFORE LOCK from=%d\n", rf.me, args.LeaderID)
	rf.mu.Lock()
	defer rf.mu.Unlock()

	reply.CommitIndex = 0
	reply.ConflictIndex = 0
	reply.ConflictTerm = -1
	reply.Success = false
	
	if args.Term < rf.currentTerm{
		reply.Term = rf.currentTerm 
		return 
	}

	if args.PrevLogIndex < rf.lastIncludedIndex{
		reply.ConflictIndex = rf.lastIncludedIndex + 1
		reply.ConflictTerm = -1
		return 
	}

	if args.PrevLogIndex == rf.lastIncludedIndex{
		if args.PrevLogTerm != rf.lastIncludedTerm{
			reply.ConflictIndex = rf.lastIncludedIndex + 1
			reply.ConflictTerm = rf.lastIncludedTerm
			return 
		}
	}

	if args.PrevLogIndex > rf.getLastIndex(){
		reply.ConflictIndex = rf.getLastIndex() + 1
		reply.ConflictTerm = -1
		return 
	}

	if args.Term > rf.currentTerm{
		rf.currentTerm = args.Term 
		rf.votedFor = -1 
	}

	rf.ChangeState(Follower)
	rf.lastHeartbeat = time.Now()
	rf.electionTimer.Reset(randomElectionTimeout())

	reply.Term = rf.currentTerm 

	// 检查日志一致性
    // 修复点：将 >= 0 改为 > rf.lastIncludedIndex
    // 如果 PrevLogIndex == lastIncludedIndex，已经在上面检查过 Term 了，不需要进这里
    // 如果 PrevLogIndex < lastIncludedIndex，也在最上面被拒绝了
    if args.PrevLogIndex > rf.lastIncludedIndex {
        
        // 边界检查保持不变
        if args.PrevLogIndex >= len(rf.log) + rf.lastIncludedIndex + 1{
            reply.ConflictIndex = len(rf.log) + rf.lastIncludedIndex + 1
            reply.ConflictTerm = -1
            return 
        }

        // 这里的计算现在安全了：PrevLogIndex > lastIncludedIndex，所以 index >= 0
        if rf.log[args.PrevLogIndex - rf.lastIncludedIndex - 1].Term != args.PrevLogTerm{
            reply.ConflictTerm = rf.log[args.PrevLogIndex - rf.lastIncludedIndex - 1].Term
            // 找到冲突 term 的第一个索引
            i := args.PrevLogIndex
            for i > rf.lastIncludedIndex + 1 && rf.log[i-1 - rf.lastIncludedIndex - 1].Term == reply.ConflictTerm{
                i--
            }
            reply.ConflictIndex = i 
            return 
        }
    }

	i := 0
	for ; i < len(args.Entries); i++{
		logIndex := args.PrevLogIndex + i + 1
		if logIndex >= len(rf.log) + rf.lastIncludedIndex + 1{
			break 
		}
		if rf.log[logIndex - rf.lastIncludedIndex - 1].Term != args.Entries[i].Term{
			rf.log = rf.log[:logIndex - rf.lastIncludedIndex - 1]
			break 
		}
	}

	logChanged := false
	if i < len(args.Entries){
		rf.log = append(rf.log, args.Entries[i:]...)
		logChanged = true
	}
	
	// 更新 commitIndex
	if args.LeaderCommit > rf.commitIndex{
		// 如果有新的 entries 被追加，commitIndex 更新到最后一个新 entry 的索引
		// 如果是心跳（没有 entries），commitIndex 更新到 PrevLogIndex
		lastNewEntry := args.PrevLogIndex + len(args.Entries)
		rf.commitIndex = min(args.LeaderCommit, lastNewEntry)
	}

	if rf.commitIndex > rf.lastApplied{
		rf.applyCond.Signal()
	}
	reply.Success = true
	reply.CommitIndex = rf.commitIndex 
	reply.Term = rf.currentTerm
	// 只在log实际被修改时才持久化
	if logChanged {
		rf.persist()
	}

	return 
}

func (rf *Raft) SendAppendEntries(server int , args *AppendEntriesArgs, reply *AppendEntriesReply) bool {	
	ok := rf.peers[server].Call("Raft.AppendEntries", args, reply)

	if !ok{
		return ok 
	}

	rf.mu.Lock()
	defer rf.mu.Unlock()
	
	// Debug: 检测长时间等待锁
	// fmt.Printf("[SAE] S%d got lock for reply from S%d\n", rf.me, server)

	if rf.state != Leader || args.Term != rf.currentTerm{
		return ok 
	}

	if reply.Term > rf.currentTerm {
		rf.currentTerm = reply.Term 
		rf.votedFor = -1
		rf.ChangeState(Follower)
		rf.persist()
		return ok 
	}

    if reply.Success {
        // 1. 【修正】基于 args 更新，而不是基于当前状态
        // 这里的 args 是当初发送 RPC 时的参数
        newMatchIndex := args.PrevLogIndex + len(args.Entries)
        
        // 防止乱序回复导致状态回退
        // 如果回复的是旧 RPC，不要减小 matchIndex
        if newMatchIndex > rf.matchIndex[server] {
            rf.matchIndex[server] = newMatchIndex
            rf.nextIndex[server] = rf.matchIndex[server] + 1
            
            // 2. 只在 matchIndex 更新时才尝试更新 commitIndex
            // 优化：只检查新匹配的这个 index 附近，而不是从头扫描
            // 从 newMatchIndex 开始向下找第一个被大多数复制的 index
            for N := newMatchIndex; N > rf.commitIndex && N > rf.lastIncludedIndex; N-- {
                // 检查 Term 是否为当前 Term (Raft 论文 5.4 安全性限制)
                offset := N - rf.lastIncludedIndex - 1
                if offset < 0 || offset >= len(rf.log) || rf.log[offset].Term != rf.currentTerm {
                    continue
                }

                count := 1
                for i := 0; i < len(rf.peers); i++ {
                    if i == rf.me {
                        continue
                    }
                    if rf.matchIndex[i] >= N {
                        count++
                    }
                }

                if count > len(rf.peers)/2 {
                    rf.commitIndex = N
                    rf.applyCond.Signal() 
                    break
                }
            }
        }
    } else {
        // 冲突回退逻辑（这部分你写得基本没问题，保留即可）
        if reply.ConflictTerm != -1 {
            lastIndex := -1
            for i := len(rf.log) - 1; i >= 0; i-- {
                if rf.log[i].Term == reply.ConflictTerm {
                    lastIndex = i
                    break
                }
            }
            if lastIndex >= 0 {
                rf.nextIndex[server] = (lastIndex + rf.lastIncludedIndex + 1) + 1
            } else {
                rf.nextIndex[server] = max(reply.ConflictIndex, rf.lastIncludedIndex+1)
            }
        } else {
            rf.nextIndex[server] = reply.ConflictIndex
        }
    }
    
    // 边界检查（保留）
    if rf.nextIndex[server] > rf.getLastIndex() + 1 {
         rf.nextIndex[server] = rf.getLastIndex() + 1
    }

    // 只有 log, votedFor, currentTerm 变化才需要 persist
    // commitIndex, nextIndex, matchIndex 变化不需要 persist
    // 所以这里其实不需要 rf.persist()，除非你在 applyLogs 里做了什么持久化
    // rf.persist() 
    
    return ok
}

func (rf *Raft) applier() {
    for !rf.killed() {
        rf.mu.Lock()
        
        // 等待 commitIndex > lastApplied
        for rf.commitIndex <= rf.lastApplied {
            rf.applyCond.Wait()
        }
        
        start := rf.lastApplied + 1
        end := rf.commitIndex
        
        // 收集需要 Apply 的消息
        var msgs []raftapi.ApplyMsg
        
        for idx := start; idx <= end; idx++ {
            // 计算在 log 切片中的偏移
            offset := idx - rf.lastIncludedIndex - 1
            
            // 安全检查
            if offset < 0 || offset >= len(rf.log) {
                continue
            }
            
            applyMsg := raftapi.ApplyMsg{
                CommandValid: true,
                Command:      rf.log[offset].Command,
                CommandIndex: idx,
            }
            msgs = append(msgs, applyMsg)
        }
        
        // 更新 lastApplied
        rf.lastApplied = end
        rf.mu.Unlock()

        // 在锁外发送到通道
        for _, m := range msgs {
            rf.applyCh <- m
        }
    }
}

// 修改后的 SendHeartBeats
// 调用此函数前必须持有 rf.mu
func (rf *Raft) SendHeartBeats() {
    // 移除开头的 rf.mu.Lock()，因为 ticker 已经持有了锁
    
    term := rf.currentTerm
    leaderCommit := rf.commitIndex
    lastIncludedIndex := rf.lastIncludedIndex
    lastIncludedTerm := rf.lastIncludedTerm
    
    // 拷贝一份 nextIndex，用于并发发送
    nextCopy := make([]int, len(rf.nextIndex))
    copy(nextCopy, rf.nextIndex)
    
    // 拷贝日志用于发送，避免并发读写 log slice
    // 注意：全量拷贝可能较慢，优化方式是只拷贝需要的或者利用不可变性，但在 Lab 中这样是安全的
    logCopy := make([]logEntry, len(rf.log))
    copy(logCopy, rf.log)
    //logLen := len(rf.log)

    for i := 0; i < len(rf.peers); i++ {
        if i == rf.me {
            continue
        }
        server := i
        
        // 启动 goroutine 发送
        // 启动 goroutine 发送
        go func(server int) {
            prevIdx := nextCopy[server] - 1
            
            // ================== 修改核心判断逻辑 ==================
            // 如果 prevIdx 小于 lastIncludedIndex，说明 Follower 落后太多，需要发快照
            if prevIdx < lastIncludedIndex {
                // 发送 InstallSnapshot
                args := InstallSnapshotArgs{
                    Term:              term,
                    LeaderID:          rf.me,
                    LastIncludedIndex: lastIncludedIndex,
                    LastIncludedTerm:  lastIncludedTerm,
                    Data:              rf.persister.ReadSnapshot(), 
                }
                reply := InstallSnapshotReply{}

                ok := rf.sendInstallSnapshot(server, &args, &reply)
                
                if !ok {
                    // RPC 失败，不更新任何状态，下次心跳会重试
                    return
                }
                
                // RPC 成功，检查回复
                rf.mu.Lock()
                defer rf.mu.Unlock()
                
                if rf.state != Leader || rf.currentTerm != term {
                    return
                }

                if reply.Term > rf.currentTerm {
                    rf.currentTerm = reply.Term
                    rf.votedFor = -1
                    rf.ChangeState(Follower)
                    rf.persist()
                    return
                }

                // 更新 nextIndex 和 matchIndex
                if args.LastIncludedIndex > rf.matchIndex[server] {
                    rf.matchIndex[server] = args.LastIncludedIndex
                    rf.nextIndex[server] = args.LastIncludedIndex + 1
                }
                return
            }
            // ================== 快照逻辑结束 ==================

            // 下面是原本的 AppendEntries 逻辑
            args := AppendEntriesArgs{
                Term:         term,
                LeaderID:     rf.me,
                LeaderCommit: leaderCommit,
            }

            // 构造 PrevLogIndex/Term 和 Entries
            // 此时 prevIdx >= lastIncludedIndex，可以安全计算 offset
            if prevIdx == lastIncludedIndex {
                args.PrevLogIndex = prevIdx
                args.PrevLogTerm = lastIncludedTerm
            } else { // prevIdx > lastIncludedIndex
                // 这里有一个边界检查，防止 nextIndex 越界
                if prevIdx <= lastIncludedIndex + len(logCopy) {
                     offset := prevIdx - lastIncludedIndex - 1
                     args.PrevLogIndex = prevIdx
                     args.PrevLogTerm = logCopy[offset].Term
                } else {
                     // 这种情况理论上不应发生，除非 nextIndex 错乱，重置为末尾
                     args.PrevLogIndex = prevIdx
                     args.PrevLogTerm = 0 
                }
            }

            // Entries
            if nextCopy[server] > lastIncludedIndex && nextCopy[server] <= lastIncludedIndex+len(logCopy) {
                offset := nextCopy[server] - lastIncludedIndex - 1
                args.Entries = append(args.Entries, logCopy[offset:]...)
            }

            reply := AppendEntriesReply{}
            rf.SendAppendEntries(server, &args, &reply) 
        }(server)
    }
}


// 修改后的 StartElection
//以此函数为准，替换原有代码。注意：调用此函数前必须持有 rf.mu
func (rf *Raft) StartElection() {
    rf.currentTerm++
    rf.state = Candidate
    rf.votedFor = rf.me
    rf.lastHeartbeat = time.Now()
    rf.persist()
    
    // 立即重置定时器（在锁内操作是安全的）
    if rf.electionTimer != nil {
        rf.electionTimer.Reset(randomElectionTimeout())
    }

    term := rf.currentTerm
    lastLogIndex := rf.getLastIndex()
    lastLogTerm := rf.getLastTerm()
    
    // 构造 Args (在锁内完成复制)
    args := RequestVoteArgs{
        Term:         term,
        CandidateID:  rf.me,
        LastLogIndex: lastLogIndex,
        LastLogTerm:  lastLogTerm,
    }

    voteGrantedSum := 1 // 给自己的一票

    for i := 0; i < len(rf.peers); i++ {
        if i == rf.me {
            continue
        }
        server := i
        // 启动 Goroutine
        go func(server int, args RequestVoteArgs) {
            // 注意：这里不需要 rf.mu.Lock() 来准备 args，因为我们已经通过参数传进来了
            // 这极大地减少了锁的竞争
            
            reply := RequestVoteReply{}
            
            ok := rf.sendRequestVote(server, &args, &reply)
            
            if !ok {
                return
            }

            // 处理回复需要加锁
            rf.mu.Lock()
            defer rf.mu.Unlock()

            // 检查状态是否已过期
            if rf.currentTerm != args.Term || rf.state != Candidate {
                return
            }

            if reply.Term > rf.currentTerm {
                rf.currentTerm = reply.Term
                rf.votedFor = -1
                rf.ChangeState(Follower) // ChangeState 内部会重置定时器
                rf.persist()
                return
            }

            if reply.VoteGranted {
                voteGrantedSum++
                if voteGrantedSum > len(rf.peers)/2 {
                    // 变为 Leader
                    rf.ChangeState(Leader)
                    rf.SendHeartBeats() // 立即发送心跳
                }
            }
        }(server, args) // Pass args by value
    }
}

// example code to send a RequestVote RPC to a server.
// server is the index of the target server in rf.peers[].
// expects RPC arguments in args.
// fills in *reply with RPC reply, so caller should
// pass &reply.
// the types of the args and reply passed to Call() must be
// the same as the types of the arguments declared in the
// handler function (including whether they are pointers).
//
// The labrpc package simulates a lossy network, in which servers
// may be unreachable, and in which requests and replies may be lost.
// Call() sends a request and waits for a reply. If a reply arrives
// within a timeout interval, Call() returns true; otherwise
// Call() returns false. Thus Call() may not return for a while.
// A false return can be caused by a dead server, a live server that
// can't be reached, a lost request, or a lost reply.
//
// Call() is guaranteed to return (perhaps after a delay) *except* if the
// handler function on the server side does not return.  Thus there
// is no need to implement your own timeouts around Call().
//
// look at the comments in ../labrpc/labrpc.go for more details.
//
// if you're having trouble getting RPC to work, check that you've
// capitalized all field names in structs passed over RPC, and
// that the caller passes the address of the reply struct with &, not
// the struct itself.
func (rf *Raft) sendRequestVote(server int, args *RequestVoteArgs, reply *RequestVoteReply) bool {
	ok := rf.peers[server].Call("Raft.RequestVote", args, reply)
	return ok
}


// the service using Raft (e.g. a k/v server) wants to start
// agreement on the next command to be appended to Raft's log. if this
// server isn't the leader, returns false. otherwise start the
// agreement and return immediately. there is no guarantee that this
// command will ever be committed to the Raft log, since the leader
// may fail or lose an election. even if the Raft instance has been killed,
// this function should return gracefully.
//
// the first return value is the index that the command will appear at
// if it's ever committed. the second return value is the current
// term. the third return value is true if this server believes it is
// the leader.
func (rf *Raft) Start(command interface{}) (int, int, bool) {
    index := -1
    term := -1
    isLeader := true

    rf.mu.Lock()
    defer rf.mu.Unlock()

    // 1. 只有 Leader 才能接收命令
    if rf.state != Leader {
        isLeader = false
        return index, term, isLeader
    }

    // 2. 构造日志条目
    e := logEntry{
        Command: command,
        Term:    rf.currentTerm,
    }
    rf.log = append(rf.log, e)

    // 3. 计算绝对索引 (关键修正)
    // 必须使用 getLastIndex()，它是基于 lastIncludedIndex 计算的绝对索引
    // 例如：lastIncludedIndex=0, len(log)=1 -> index=1
    index = rf.getLastIndex()
    term = rf.currentTerm
    
    // 4. 更新 Leader 自己的 matchIndex
    rf.matchIndex[rf.me] = index
    rf.nextIndex[rf.me] = index + 1

    rf.persist()
    
    // 5. 立即广播心跳/日志
    rf.SendHeartBeats()

    return index, term, isLeader
}


// the tester doesn't halt goroutines created by Raft after each test,
// but it does call the Kill() method. your code can use killed() to
// check whether Kill() has been called. the use of atomic avoids the
// need for a lock.
//
// the issue is that long-running goroutines use memory and may chew
// up CPU time, perhaps causing later tests to fail and generating
// confusing debug output. any goroutine with a long-running loop
// should call killed() to check whether it should stop.
func (rf *Raft) Kill() {
	atomic.StoreInt32(&rf.dead, 1)
	// Your code here, if desired.
}

func (rf *Raft) killed() bool {
	z := atomic.LoadInt32(&rf.dead)
	return z == 1
}

func (rf *Raft) ticker() {
    for rf.killed() == false {
        select {
        case <-rf.electionTimer.C:
            rf.mu.Lock() // 获取锁
            if rf.state != Leader {
                // StartElection 内部不再加锁，而是假定持有锁
                // StartElection 内部会重置 electionTimer
                rf.StartElection()
            } else {
                // 如果已经是 Leader，重置定时器（防止状态切换导致的逻辑漏洞）
                 rf.electionTimer.Reset(randomElectionTimeout())
            }
            rf.mu.Unlock() // 释放锁

        case <-rf.heartbeatTimer.C:
            rf.mu.Lock() // 获取锁
            if rf.state == Leader {
                // SendHeartBeats 内部不再加锁
                rf.SendHeartBeats()
                rf.heartbeatTimer.Reset(heartbeatTimeout())
            }
            rf.mu.Unlock() // 释放锁
        }
    }
}

// the service or tester wants to create a Raft server. the ports
// of all the Raft servers (including this one) are in peers[]. this
// server's port is peers[me]. all the servers' peers[] arrays
// have the same order. persister is a place for this server to
// save its persistent state, and also initially holds the most
// recent saved state, if any. applyCh is a channel on which the
// tester or service expects Raft to send ApplyMsg messages.
// Make() must return quickly, so it should start goroutines
// for any long-running work.
func Make(peers []*labrpc.ClientEnd, me int,
    persister *tester.Persister, applyCh chan raftapi.ApplyMsg) raftapi.Raft {
    rf := &Raft{}
    rf.peers = peers
    rf.persister = persister
    rf.me = me

    rf.currentTerm = 0
    rf.votedFor = -1 
    rf.log = make([]logEntry, 0)

    rf.commitIndex = 0 // 初始化为 0
    rf.lastApplied = 0 // 初始化为 0

    rf.nextIndex = make([]int, len(rf.peers))
    rf.matchIndex = make([]int, len(rf.peers))

    rf.state = Follower 
    rf.electionTimer = time.NewTimer(randomElectionTimeout())
    rf.heartbeatTimer = time.NewTimer(heartbeatTimeout())
    rf.lastHeartbeat = time.Now()
    rf.applyCh = applyCh 
    rf.applyCond = sync.NewCond(&rf.mu)

    // ---------------- 关键修正 ----------------
    // 初始状态下，快照索引为 0，日志为空
    // 这样 Append 第一条日志后，Index 就是 1
    rf.lastIncludedIndex = 0 
    rf.lastIncludedTerm = 0
    // ----------------------------------------

    // 读取持久化状态 (如果读取失败，上面的 0 依然有效)
    rf.readPersist(persister.ReadRaftState())

    // 恢复 lastApplied (至少要等于快照进度)
    if rf.lastIncludedIndex > 0 {
        rf.lastApplied = rf.lastIncludedIndex
        rf.commitIndex = rf.lastIncludedIndex
    }

    go rf.ticker()
    go rf.applier()

    return rf
}