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
	//	"bytes"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"
	//"fmt"

	//	"6.5840/labgob"
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
	case Follower, Candidate:
		if rf.heartbeatTimer != nil{
			rf.heartbeatTimer.Stop()
		}
		if rf.electionTimer != nil{
			rf.electionTimer.Reset(randomElectionTimeout())
		}
	case Leader:
		if rf.electionTimer != nil{
			rf.electionTimer.Stop()
		}
		if rf.heartbeatTimer != nil{
			rf.heartbeatTimer.Reset(heartbeatTimeout())
		}
	}

		//对nextIndex / matchIndex 进行初始化
		lastIndex := len(rf.log) - 1 // 最后一条日志的下标，可能为 -1
		for i := range rf.peers {
			rf.nextIndex[i] = len(rf.log) // 下一条要发的下标，0..len
			rf.matchIndex[i] = -1           // 初始没有复制任何日志
		}
		rf.matchIndex[rf.me] = lastIndex
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
//rf是候选人，args记载了请求投票的相关信息
func (rf *Raft) RequestVote(args *RequestVoteArgs, reply *RequestVoteReply) {
	// Your code here (3A, 3B).
	rf.mu.Lock()
	defer rf.mu.Unlock()

	if args.Term < rf.currentTerm {
		reply.VoteGranted = false 
		reply.Term = rf.currentTerm 
		return 
	}

	if args.Term > rf.currentTerm {
		rf.currentTerm = args.Term 
		rf.votedFor = -1
		rf.ChangeState(Follower)
	}

	lastIndex := len(rf.log) - 1
	lastTerm := 0
	if lastIndex >= 0{
		lastTerm = rf.log[lastIndex].Term 
	}

	upToDate := (args.LastLogTerm > lastTerm ) || (args.LastLogTerm == lastTerm && args.LastLogIndex >= lastIndex)

	if (rf.votedFor == -1 || rf.votedFor == args.CandidateID) && upToDate {
		rf.votedFor = args.CandidateID
		reply.VoteGranted = true 
		reply.Term = rf.currentTerm
		// 授予选票：认为看到了“领导活动”，延后自身选举
		if rf.electionTimer != nil {
			rf.electionTimer.Reset(randomElectionTimeout())
		}
		rf.lastHeartbeat = time.Now()
	} else {
		reply.VoteGranted = false 
		reply.Term = rf.currentTerm
	}
	return
}

func (rf *Raft) AppendEntries(args *AppendEntriesArgs, reply *AppendEntriesReply){
	rf.mu.Lock()
	defer rf.mu.Unlock()

	//fmt.Printf("AE recv: me=%d term=%d from=%d args={Term=%d PrevIdx=%d PrevTerm=%d LeaderCommit=%d entries=%d} logLen=%d commit=%d\n",
    //    rf.me, rf.currentTerm, args.LeaderID,
    //    args.Term, args.PrevLogIndex, args.PrevLogTerm, args.LeaderCommit, len(args.Entries),
     //   len(rf.log), rf.commitIndex)
	reply.CommitIndex = 0
	reply.Term = rf.currentTerm
	reply.Success = false
	reply.ConflictTerm = -1
	reply.ConflictIndex = 0

	if args.Term < rf.currentTerm{	
		//fmt.Printf("AE reject: me=%d reason=oldTerm argsTerm=%d curTerm=%d\n", rf.me, args.Term, rf.currentTerm)	
		return 
	}

	if args.Term > rf.currentTerm{
		rf.currentTerm = args.Term 
		rf.votedFor = -1
	}



	// 收到合法 AppendEntries：保持/切换为 follower，并刷新选举计时
	rf.ChangeState(Follower)
	rf.lastHeartbeat = time.Now()
	if rf.electionTimer != nil {
		rf.electionTimer.Reset(randomElectionTimeout())
	}


	// 0-based：PrevLogIndex 为 -1 表示“前面没有日志”
	if args.PrevLogIndex >= 0 {
		if args.PrevLogIndex >= len(rf.log) {
		    //fmt.Printf("AE reject: me=%d reason=shortLog prevIdx=%d logLen=%d\n",
        //rf.me, args.PrevLogIndex, len(rf.log))
			reply.Success = false 
			reply.ConflictTerm = -1
			reply.ConflictIndex = len(rf.log)
			return
		}
		if rf.log[args.PrevLogIndex].Term != args.PrevLogTerm {
			confTerm := rf.log[args.PrevLogIndex].Term
			firstIndex := args.PrevLogIndex
			for firstIndex > 0 && rf.log[firstIndex-1].Term == confTerm{
				firstIndex--
			}
			//fmt.Printf("AE reject: me=%d reason=termMismatch prevIdx=%d prevTerm=%d localTerm=%d confFirst=%d\n",
        //rf.me, args.PrevLogIndex, args.PrevLogTerm, confTerm, firstIndex)
			reply.Success = false 
			reply.ConflictTerm = confTerm 
			reply.ConflictIndex = firstIndex 
			return
		}
	}
	

	// 从 PrevLogIndex+1 开始接收新日志，覆盖冲突部分
	insertIndex := args.PrevLogIndex + 1
	i := 0
	for ; i < len(args.Entries); i++ {
		pos := insertIndex + i
		if pos >= len(rf.log){
			break
		}
		if rf.log[pos].Term != args.Entries[i].Term{
			rf.log = rf.log[:pos]
			break 
		}
	}

	if i < len(args.Entries){
		rf.log = append(rf.log, args.Entries[i:]...)
	}

	lastNewIndex := len(rf.log) - 1
	if args.LeaderCommit > rf.commitIndex{
		if args.LeaderCommit < lastNewIndex{
			rf.commitIndex = args.LeaderCommit
		}else{
			rf.commitIndex = lastNewIndex 
		}
	}

	reply.Term = rf.currentTerm
	reply.Success = true

	//fmt.Printf("AE accept: me=%d newLogLen=%d commit=%d lastApplied=%d\n",
    //rf.me, len(rf.log), rf.commitIndex, rf.lastApplied)

	if rf.commitIndex > rf.lastApplied{
		go rf.applyLogs()
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

	if rf.state != Leader || args.Term < rf.currentTerm{
		return ok 
	}

	if rf.currentTerm < reply.Term{
		rf.currentTerm = reply.Term 
		rf.ChangeState(Follower)
		rf.votedFor = -1
		return ok 
	}

	if reply.Success {
		// args.PrevLogIndex / nextIndex / matchIndex 都是下标
		rf.nextIndex[server] = args.PrevLogIndex + len(args.Entries) + 1
		if rf.nextIndex[server] > len(rf.log) {
			rf.nextIndex[server] = len(rf.log)
		}
		rf.matchIndex[server] = rf.nextIndex[server] - 1

		// 推进 commitIndex：N 是下标
		for N := rf.commitIndex + 1; N < len(rf.log); N++ {
			cnt := 1 // 自己
			for i := 0; i < len(rf.peers); i++ {
				if i != rf.me && rf.matchIndex[i] >= N {
					cnt++
				}
			}
			if cnt > len(rf.peers)/2 && rf.log[N].Term == rf.currentTerm {
				rf.commitIndex = N
			}
		}
		if rf.commitIndex > rf.lastApplied {
			go rf.applyLogs()
		}
	} else {
		//fmt.Printf("AE fail@leader: me=%d to=%d term=%d nextIdx(before)=%d conflictTerm=%d conflictIndex=%d logLen=%d\n",
        //rf.me, server, rf.currentTerm, rf.nextIndex[server],
        //reply.ConflictTerm, reply.ConflictIndex, len(rf.log))

		// 利用 follower 提供的冲突信息快速回退
		if reply.ConflictTerm == -1 {
			// follower 日志太短：直接把 nextIndex 退到 ConflictIndex
			if reply.ConflictIndex >= 0 && reply.ConflictIndex <= len(rf.log) {
				rf.nextIndex[server] = reply.ConflictIndex
			} else if reply.ConflictIndex < 0 {
				rf.nextIndex[server] = 0
			} else {
				rf.nextIndex[server] = len(rf.log)
			}
		} else {
			// follower 有冲突 term：在本地日志里找该 term 的最后一个条目
			lastIndex := -1
			for i := len(rf.log) - 1; i >= 0; i-- {
				if rf.log[i].Term == reply.ConflictTerm {
					lastIndex = i
					break
				}
			}
			if lastIndex >= 0 {
				// leader 也有这个 term：从这个 term 之后开始发
				rf.nextIndex[server] = lastIndex + 1
			} else {
				// leader 没有这个 term：直接退到 follower 建议的 ConflictIndex
				rf.nextIndex[server] = reply.ConflictIndex
			}
		}

		if rf.nextIndex[server] < 0 {
			rf.nextIndex[server] = 0
		}
		if rf.nextIndex[server] > len(rf.log) {
			rf.nextIndex[server] = len(rf.log)
		}
	}

	return ok
}

func (rf *Raft) applyLogs(){
	rf.mu.Lock()
	defer rf.mu.Unlock()

	if rf.commitIndex > len(rf.log) - 1 {
		return
	}

	for i := rf.lastApplied + 1; i <= rf.commitIndex; i++ {
		rf.applyCh <- raftapi.ApplyMsg{
			CommandValid: true,
			Command:      rf.log[i].Command,
			CommandIndex: i + 1,
		}
	}
	rf.lastApplied = rf.commitIndex
}

func (rf *Raft) SendHeartBeats() {
    for peer := 0; peer < len(rf.peers); peer++ {
        if peer == rf.me {
            continue
        }
        go func(peer int) {
            rf.mu.Lock()
            if rf.state != Leader {
                rf.mu.Unlock()
                return
            }

            args := AppendEntriesArgs{
                Term:         rf.currentTerm,
                LeaderID:     rf.me,
                LeaderCommit: rf.commitIndex,
            }

            nextIdx := rf.nextIndex[peer]      // 0-based: 下一条要发的下标, 0..len(log)-1
            prevIdx := nextIdx - 1             // 0-based: 上一条的下标, 可能为 -1

            if prevIdx >= 0 && prevIdx < len(rf.log) {
                args.PrevLogIndex = prevIdx
                args.PrevLogTerm = rf.log[prevIdx].Term
            } else {
                // 日志前无条目
                args.PrevLogIndex = -1
                args.PrevLogTerm = 0
            }

            if nextIdx >= 0 && nextIdx < len(rf.log) {
                args.Entries = append(args.Entries, rf.log[nextIdx:]...)
            } else {
                args.Entries = nil
            }

            rf.mu.Unlock()

            reply := AppendEntriesReply{}
            ok := rf.SendAppendEntries(peer, &args, &reply)
            if !ok {
                return
            }

            rf.mu.Lock()
            defer rf.mu.Unlock()
            if reply.Term > rf.currentTerm {
                rf.currentTerm = reply.Term
                rf.ChangeState(Follower)
            }
        }(peer)
    }
}

func (rf *Raft) StartElection() {
    //fmt.Printf("ELECT: me=%d start election term=%d logLen=%d\n", rf.me, rf.currentTerm, len(rf.log))

    // 自己进入 Candidate，term+1
    rf.state = Candidate
    rf.currentTerm++
    rf.votedFor = rf.me
    rf.lastHeartbeat = time.Now()

    // 重新设置选举超时
    if rf.electionTimer != nil {
        rf.electionTimer.Reset(randomElectionTimeout())
    }

    term := rf.currentTerm
    voteGrantedSum := 1

    for peer := 0; peer < len(rf.peers); peer++ {
        if peer == rf.me {
            continue
        }
        go func(peer int) {
            args := RequestVoteArgs{Term: term, CandidateID: rf.me}
            reply := RequestVoteReply{}

            rf.mu.Lock()
            if len(rf.log) > 0 {
                args.LastLogIndex = len(rf.log) - 1
                args.LastLogTerm = rf.log[len(rf.log)-1].Term
            }
            rf.mu.Unlock()

            ok := rf.sendRequestVote(peer, &args, &reply)
            if !ok {
                return
            }

            rf.mu.Lock()
            defer rf.mu.Unlock()

            if rf.currentTerm != term || rf.state != Candidate {
                return
            }

            if reply.Term > rf.currentTerm {
                rf.currentTerm = reply.Term
                rf.ChangeState(Follower)
                rf.votedFor = -1
                return
            }

            if reply.VoteGranted {
                voteGrantedSum++
                if voteGrantedSum > len(rf.peers)/2 && rf.state == Candidate {
                    rf.ChangeState(Leader)
                    rf.SendHeartBeats()
                }
            }
        }(peer)
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

	e := logEntry{Command: command, Term: rf.currentTerm}
	rf.log = append(rf.log, e)

	lastIndex := len(rf.log) - 1
	rf.matchIndex[rf.me] = lastIndex 

	index = len(rf.log) 
	term = rf.currentTerm 

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

		// Your code here (3A)
		// Check if a leader election should be started.
		select {
		case <-rf.electionTimer.C:
			rf.mu.Lock()
			if rf.state != Leader {
				rf.StartElection()
			}
			rf.mu.Unlock()

		case <-rf.heartbeatTimer.C:
			rf.mu.Lock()
			if rf.state == Leader {
				rf.SendHeartBeats()
				rf.heartbeatTimer.Reset(heartbeatTimeout())
			}
			rf.mu.Unlock()
		}

		// pause for a random amount of time between 50 and 350
		// milliseconds.
		//ms := 10 + (rand.Int63() % 40)
		//time.Sleep(time.Duration(ms) * time.Millisecond)
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

	rf.nextIndex = make([]int, len(peers))
	rf.matchIndex = make([]int, len(peers))

	rf.state = Follower
	rf.lastHeartbeat = time.Now()
	rf.electionTimer = time.NewTimer(randomElectionTimeout())
	rf.heartbeatTimer = time.NewTimer(heartbeatTimeout())

	rf.applyCh = applyCh

	// initialize from state persisted before a crash
	rf.readPersist(persister.ReadRaftState())

	// start ticker goroutine to start elections
	go rf.ticker()


	return rf
}
