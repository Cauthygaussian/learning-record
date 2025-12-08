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
	isleader = (rf.state == Leader)
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
func (rf *Raft) ChangeState(statement int){
	if rf.state == statement{
		return 
	}
	rf.state = statement 
	switch statement{
	case Leader:
		if rf.heartbeatTimer != nil{
			rf.heartbeatTimer.Reset(heartbeatTimeout())
		}
		if rf.electionTimer != nil{
			rf.electionTimer.Stop()
		}
		for i := 0; i < len(rf.peers); i++{
			rf.nextIndex[i] = len(rf.log)
			rf.matchIndex[i] = -1
		}
		rf.matchIndex[rf.me] = len(rf.log) - 1

	case Follower, Candidate:
		if rf.heartbeatTimer != nil{
			rf.heartbeatTimer.Stop()
		}
		if rf.electionTimer != nil{
			rf.electionTimer.Reset(randomElectionTimeout())
		}
	}

	

	return 
}



// save Raft's persistent state to stable storage,
// where it can later be retrieved after a crash and restart.
// see paper's Figure 2 for a description of what should be persistent.
// before you've implemented snapshots, you should pass nil as the
// second argument to persister.Save().
// after you've implemented snapshots, pass the current snapshot
// (or nil if there's not yet a snapshot).
func (rf *Raft) persist() {
	// Your code here (3C).
	// Example:
	// w := new(bytes.Buffer)
	// e := labgob.NewEncoder(w)
	// e.Encode(rf.xxx)
	// e.Encode(rf.yyy)
	// raftstate := w.Bytes()
	// rf.persister.Save(raftstate, nil)
	w := new(bytes.Buffer)
	e := labgob.NewEncoder(w)
	e.Encode(rf.currentTerm)
	e.Encode(rf.votedFor)
	e.Encode(rf.log)
	raftstate := w.Bytes()
	rf.persister.Save(raftstate, nil)
}


// restore previously persisted state.
func (rf *Raft) readPersist(data []byte) {
	if data == nil || len(data) < 1 { // bootstrap without any state?
		return
	}
	// Your code here (3C).
	// Example:
	// r := bytes.NewBuffer(data)
	// d := labgob.NewDecoder(r)
	// var xxx
	// var yyy
	// if d.Decode(&xxx) != nil ||
	//    d.Decode(&yyy) != nil {
	//   error...
	// } else {
	//   rf.xxx = xxx
	//   rf.yyy = yyy
	// }
	r := bytes.NewBuffer(data)
	d := labgob.NewDecoder(r)
	var currentTerm int 
	var votedFor int 
	var log []logEntry 

	if d.Decode(&currentTerm) != nil || d.Decode(&votedFor) != nil || d.Decode(&log) != nil {
		fmt.Println("decode error!")
	}else{
		rf.currentTerm = currentTerm
		rf.votedFor = votedFor 
		rf.log = log 
	}
}

// how many bytes in Raft's persisted log?
func (rf *Raft) PersistBytes() int {
	rf.mu.Lock()
	defer rf.mu.Unlock()
	return rf.persister.RaftStateSize()
}


// the service says it has created a snapshot that has
// all info up to and including index. this means the
// service no longer needs the log through (and including)
// that index. Raft should now trim its log as much as possible.
func (rf *Raft) Snapshot(index int, snapshot []byte) {
	// Your code here (3D).

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

// example RequestVote RPC handler.
//rf是投票的那个，args记载了请求投票的相关信息
func (rf *Raft) RequestVote(args *RequestVoteArgs, reply *RequestVoteReply) {
	// Your code here (3A, 3B).
	rf.mu.Lock()
	defer rf.mu.Unlock()

	reply.VoteGranted = false

	if args.Term < rf.currentTerm{
		reply.Term = rf.currentTerm 
		return 
	}

	if args.Term > rf.currentTerm{
		rf.currentTerm = args.Term 
		rf.votedFor = -1
		rf.ChangeState(Follower)
	}

	reply.Term = rf.currentTerm 
	lastIndex := len(rf.log) - 1 
	lastTerm := 0
	if lastIndex >= 0{
		lastTerm = rf.log[lastIndex].Term 
	}

	// 判断日志是否至少和自己一样新
	upToDate := (args.LastLogTerm > lastTerm) || (args.LastLogTerm == lastTerm && args.LastLogIndex >= lastIndex)

	if (rf.votedFor == -1 || rf.votedFor == args.CandidateID) && upToDate{
		rf.votedFor = args.CandidateID 
		rf.lastHeartbeat = time.Now()
		rf.electionTimer.Reset(randomElectionTimeout())

		reply.VoteGranted = true
	}

	rf.persist()
	
	return
}

func (rf *Raft) AppendEntries(args *AppendEntriesArgs, reply *AppendEntriesReply){
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

	if args.Term > rf.currentTerm{
		rf.currentTerm = args.Term 
		rf.votedFor = -1 
		
	}

	rf.ChangeState(Follower)
	rf.lastHeartbeat = time.Now()
	rf.electionTimer.Reset(randomElectionTimeout())

	reply.Term = rf.currentTerm 

	// 检查日志一致性
	if args.PrevLogIndex >= 0{
		if args.PrevLogIndex >= len(rf.log){
			reply.ConflictIndex = len(rf.log)
			reply.ConflictTerm = -1
			return 
		}

		if rf.log[args.PrevLogIndex].Term != args.PrevLogTerm{
			reply.ConflictTerm = rf.log[args.PrevLogIndex].Term
			// 找到冲突 term 的第一个索引
			i := args.PrevLogIndex 
			for i > 0 && rf.log[i-1].Term == reply.ConflictTerm{
				i--
			}
			reply.ConflictIndex = i 
			return 
		}
	}

	i := 0
	for ; i < len(args.Entries); i++{
		logIndex := args.PrevLogIndex + i + 1
		if logIndex >= len(rf.log){
			break 
		}
		if rf.log[logIndex].Term != args.Entries[i].Term{
			rf.log = rf.log[:logIndex]
			break 
		}
	}

	if i < len(args.Entries){
		rf.log = append(rf.log, args.Entries[i:]...)
	}
	

	// 更新 commitIndex
	if args.LeaderCommit > rf.commitIndex{
		rf.commitIndex = min(args.LeaderCommit, len(rf.log) - 1)
	}

	if rf.commitIndex > rf.lastApplied{
		go rf.applyLogs()
	}

	reply.Success = true
	reply.CommitIndex = rf.commitIndex 
	reply.Term = rf.currentTerm
	rf.persist()

	return 
}

func (rf *Raft) SendAppendEntries(server int , args *AppendEntriesArgs, reply *AppendEntriesReply) bool {	
	ok := rf.peers[server].Call("Raft.AppendEntries", args, reply)

	if !ok{
		return ok 
	}

	rf.mu.Lock()
	defer rf.mu.Unlock()

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

	if reply.Success{
		// 更新 nextIndex 和 matchIndex
		rf.nextIndex[server] = args.PrevLogIndex + len(args.Entries) + 1
		if rf.nextIndex[server] > len(rf.log){
			rf.nextIndex[server] = len(rf.log)
		}

		rf.matchIndex[server] = rf.nextIndex[server] - 1

		// 更新 commitIndex
		for commitIndex := rf.commitIndex + 1; commitIndex < len(rf.log); commitIndex++{
			count := 1
			for i := 0; i < len(rf.peers); i++{
				if i == rf.me{
					continue 
				}
				if rf.matchIndex[i] >= commitIndex{
					count++
				}
			}
			if count > len(rf.peers) / 2 && rf.log[commitIndex].Term == rf.currentTerm{
				rf.commitIndex = commitIndex 
			}
		}
		if rf.commitIndex > rf.lastApplied{
			go rf.applyLogs()
		}
	}else{
		// 回退 nextIndex
		if reply.ConflictTerm != -1{
			// 在日志中查找冲突的 term
			lastIndex := -1 
			for i := len(rf.log) - 1; i >= 0; i--{
				if rf.log[i].Term == reply.ConflictTerm{
					lastIndex = i
					break 
				}
			}
			if lastIndex >= 0{
				rf.nextIndex[server] = lastIndex + 1
			}else {
				rf.nextIndex[server] = reply.ConflictIndex
			}
		}else{
			if reply.ConflictIndex < 0{
				rf.nextIndex[server] = 0
			}else if reply.ConflictIndex > len(rf.log){
				rf.nextIndex[server] = len(rf.log)
			}else{
				rf.nextIndex[server] = reply.ConflictIndex 
			}
			
		}

		if rf.nextIndex[server] < 0{
			rf.nextIndex[server] = 0
		}else if rf.nextIndex[server] > len(rf.log){
			rf.nextIndex[server] = len(rf.log)
		}
	}

	rf.persist()
	return ok 
}

func (rf *Raft) applyLogs(){
	rf.mu.Lock()
	defer rf.mu.Unlock()

	if rf.commitIndex > len(rf.log) -1 {
		return 
	}

	if rf.commitIndex > rf.lastApplied{
		for i := rf.lastApplied + 1; i <= rf.commitIndex; i++{
			applyMsg := raftapi.ApplyMsg{
				CommandValid: true,
				Command: rf.log[i].Command,
				CommandIndex: i + 1,
			}
			rf.applyCh <- applyMsg
		}
		rf.lastApplied = rf.commitIndex
	}
	return 
}

func (rf *Raft) SendHeartBeats() {
	term := rf.currentTerm 
   	for i := 0; i < len(rf.peers); i++{
		if i == rf.me{
			continue 
		}

		go func(server int){
			rf.mu.Lock()
			args := AppendEntriesArgs{
				Term: rf.currentTerm,
				LeaderID: rf.me,
				LeaderCommit: rf.commitIndex,
			}

			reply := AppendEntriesReply{}

			nextIdx := rf.nextIndex[server]
			prevIdx := nextIdx - 1

			if prevIdx >= 0 && prevIdx < len(rf.log){
				args.PrevLogIndex = prevIdx 
				args.PrevLogTerm = rf.log[prevIdx].Term 
			}else {
				args.PrevLogIndex = -1
				args.PrevLogTerm = 0
			}

			if nextIdx >=0 && nextIdx < len(rf.log){
				args.Entries = append(args.Entries, rf.log[nextIdx:]...)
			}else{
				args.Entries = nil 
			}

			// 检查任期和状态
			if term != rf.currentTerm || rf.state != Leader{
				rf.mu.Unlock()
				return // 任期已更改或不再是领导人
			}

			rf.mu.Unlock()

			ok := rf.SendAppendEntries(server, &args, &reply)

			if !ok{
				return // RPC调用失败
			}

			rf.mu.Lock()
			defer rf.mu.Unlock()
			if reply.Term > rf.currentTerm{
				rf.currentTerm = reply.Term 
				rf.votedFor = -1
				rf.ChangeState(Follower)
				 // 收到更高的任期，回到跟随者状态
			}

		}(i)
   }
}


func (rf *Raft) StartElection() {
    //fmt.Printf("ELECT: me=%d start election term=%d logLen=%d\n", rf.me, rf.currentTerm, len(rf.log))

    // 自己进入 Candidate，term+1
	rf.votedFor = rf.me 
	rf.currentTerm += 1
	rf.state = Candidate 
	rf.lastHeartbeat = time.Now()

	if rf.electionTimer != nil{
		rf.electionTimer.Reset(randomElectionTimeout())
	}

	voteGrantedSum := 1 // 给自己投票
	term := rf.currentTerm 
	rf.persist()
	// 向其他所有服务器发送 RequestVote RPC
	
	for i := 0; i < len(rf.peers); i++{
		if i == rf.me{
			continue 
		}

		go func (server int){

			rf.mu.Lock()
			args := RequestVoteArgs{
				Term: term,
				CandidateID: rf.me,
			}

			if len(rf.log) > 0{
				args.LastLogIndex = len(rf.log) - 1
				args.LastLogTerm = rf.log[args.LastLogIndex].Term 
			}
			reply := RequestVoteReply{}

			rf.mu.Unlock()

			ok := rf.sendRequestVote(server, &args, &reply)

			if !ok{
				return // RPC调用失败
			}

			rf.mu.Lock()
			defer rf.mu.Unlock()

			if term != rf.currentTerm || rf.state != Candidate{
				return // 任期已更改或不再是候选人
			}

			if reply.Term > rf.currentTerm{
				rf.currentTerm = reply.Term
				rf.votedFor = -1 
				rf.ChangeState(Follower)

				rf.persist()
				return // 收到更高的任期，回到跟随者状态
			}

			if reply.VoteGranted{
				voteGrantedSum++
				if voteGrantedSum > len(rf.peers) / 2{
					// 获得多数票，成为领导人
					rf.ChangeState(Leader)
					rf.SendHeartBeats()
				}
			}

			

		}(i)
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

	// Your code here (3B).
	rf.mu.Lock()
	defer rf.mu.Unlock()

	if rf.state != Leader{
		isLeader = false 
		return index, term, isLeader 
	}

	e := logEntry{
		Command: command,
		Term:    rf.currentTerm,
	}
	rf.log = append(rf.log, e)

	lastIndex := len(rf.log) - 1 
	rf.matchIndex[rf.me] = lastIndex 

	index = len(rf.log)
	term = rf.currentTerm 

	rf.persist()

	return index, term ,isLeader
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

		// Your code here (3A)
		// Check if a leader election should be started.
		select{
		case <- rf.electionTimer.C:
			if rf.state != Leader{
				rf.StartElection()
			}
		case <- rf.heartbeatTimer.C:
			if rf.state == Leader{
				rf.SendHeartBeats()
				rf.heartbeatTimer.Reset(heartbeatTimeout())
			}
		}


		// pause for a random amount of time between 10 and 50
		// milliseconds.
		//ms := 10 + (rand.Int63() % 40)
		ms := rand.Int63() % 10 
		time.Sleep(time.Duration(ms) * time.Millisecond)
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

	// Your initialization code here (3A, 3B, 3C).
	rf.currentTerm = 0
	rf.votedFor = -1 
	rf.log = make([]logEntry, 0)

	rf.commitIndex = -1 
	rf.lastApplied = -1 

	rf.nextIndex = make([]int, len(rf.peers))
	rf.matchIndex = make([]int, len(rf.peers))

	rf.state = Follower 
	rf.electionTimer = time.NewTimer(randomElectionTimeout())
	rf.heartbeatTimer = time.NewTimer(heartbeatTimeout())
	rf.lastHeartbeat = time.Now()
	rf.applyCh = applyCh 
	rf.applyCond = sync.NewCond(&rf.mu)

	// initialize from state persisted before a crash
	rf.readPersist(persister.ReadRaftState())

	// start ticker goroutine to start elections
	go rf.ticker()


	return rf
}
