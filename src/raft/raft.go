package raft

//
// this is an outline of the API that raft must expose to
// the service (or tester). see comments below for
// each of these functions for more details.
//
// rf = Make(...)
//   create a new Raft server.
// rf.Start(command interface{}) (index, term, isleader)
//   start agreement on a new log entry
// rf.GetState() (term, isLeader)
//   ask a Raft for its current term, and whether it thinks it is leader
// ApplyMsg
//   each time a new entry is committed to the log, each Raft peer
//   should send an ApplyMsg to the service (or tester)
//   in the same server.
//

import (
	"bytes"
	"fmt"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"

	"6.5840/labgob"
	"6.5840/labrpc"
)

const (
	Follower  = 0
	Candidate = 1
	Leader    = 2
)

// as each Raft peer becomes aware that successive log entries are
// committed, the peer should send an ApplyMsg to the service (or
// tester) on the same server, via the applyCh passed to Make(). set
// CommandValid to true to indicate that the ApplyMsg contains a newly
// committed log entry.
//
// in part 2D you'll want to send other kinds of messages (e.g.,
// snapshots) on the applyCh, but set CommandValid to false for these
// other uses.
type ApplyMsg struct {
	CommandValid bool
	Command      interface{}
	CommandIndex int

	// For 2D:
	SnapshotValid bool
	Snapshot      []byte
	SnapshotTerm  int
	SnapshotIndex int
}
type Log struct {
	Term    int
	Command interface{}
}

type Time struct {
	initTime time.Time
	duration time.Duration
}

func (t *Time) reset() {
	minDuration := 200 * time.Millisecond
	maxDuration := 250 * time.Millisecond
	t.duration = time.Duration(rand.Int63n(int64(maxDuration-minDuration))) + minDuration
	t.initTime = time.Now()
}

// A Go object implementing a single Raft peer.
type Raft struct {
	mu        sync.Mutex          // Lock to protect shared access to this peer's state
	peers     []*labrpc.ClientEnd // RPC end points of all peers
	persister *Persister          // Object to hold this peer's persisted state
	me        int                 // this peer's index into peers[]
	dead      int32               // set by Kill()

	// Your data here (2A, 2B, 2C).
	// Look at the paper's Figure 2 for a description of what
	// state a Raft server must maintain.

	// Persistent state on all servers:
	currentTerm int
	votedFor    int
	log         []Log
	// for snapshot
	lastIncludedIndex int
	lastIncludedTerm  int

	// Volatile state on all servers:
	commitIndex int
	lastApplied int
	snapShot    []byte

	// Volatile state on leaders:
	nextIndex  []int
	matchIndex []int

	// Other states:
	rule      int
	voteCount int
	time      *Time
	applyChan chan ApplyMsg
}

// return currentTerm and whether this server
// believes it is the leader.
func (rf *Raft) GetState() (int, bool) {
	var term int
	var isleader bool
	// Your code here (2A).
	rf.mu.Lock()
	defer rf.mu.Unlock()
	term = rf.currentTerm
	isleader = rf.rule == Leader
	return term, isleader
}

// save Raft's persistent state to stable storage,
// where it can later be retrieved after a crash and restart.
// see paper's Figure 2 for a description of what should be persistent.
// before you've implemented snapshots, you should pass nil as the
// second argument to persister.Save().
// after you've implemented snapshots, pass the current snapshot
// (or nil if there's not yet a snapshot).
func (rf *Raft) persist(containingSnapShot bool) {
	rf.mu.Lock()
	defer rf.mu.Unlock()
	w := new(bytes.Buffer)
	e := labgob.NewEncoder(w)
	e.Encode(rf.currentTerm)
	e.Encode(rf.votedFor)
	e.Encode(rf.log)
	e.Encode(rf.lastIncludedIndex)
	e.Encode(rf.lastIncludedTerm)
	raftstate := w.Bytes()
	if containingSnapShot {
		rf.persister.Save(raftstate, rf.snapShot)
	} else {
		rf.persister.Save(raftstate, nil)
	}
}

// restore previously persisted state.
func (rf *Raft) readPersist(data []byte) {
	DPrintf("server %v read persist!!!", rf.me)
	if data == nil || len(data) < 1 { // bootstrap without any state?
		return
	}
	rf.mu.Lock()
	defer rf.mu.Unlock()
	r := bytes.NewBuffer(data)
	d := labgob.NewDecoder(r)
	var currentTerm int
	var votedFor int
	var log []Log
	var lastIncludedIndex int
	var lastIncludedTerm int
	if d.Decode(&currentTerm) != nil ||
		d.Decode(&votedFor) != nil ||
		d.Decode(&log) != nil ||
		d.Decode(&lastIncludedIndex) != nil ||
		d.Decode(&lastIncludedTerm) != nil {
		fmt.Println("readPersist Decode failed!")
	} else {
		rf.currentTerm = currentTerm
		rf.votedFor = votedFor
		rf.log = log
		rf.lastIncludedIndex = lastIncludedIndex
		rf.lastIncludedTerm = lastIncludedTerm
		rf.commitIndex = lastIncludedIndex
		rf.lastApplied = lastIncludedIndex
	}
}

// the service says it has created a snapshot that has
// all info up to and including index. this means the
// service no longer needs the log through (and including)
// that index. Raft should now trim its log as much as possible.
func (rf *Raft) Snapshot(index int, snapshot []byte) {
	// Your code here (2D).
	rf.mu.Lock()
	defer rf.mu.Unlock()
	index--
	// Not necessary(the old snapshot contains all needed info)
	if index <= rf.lastIncludedIndex {
		return
	}

	// Trim the Log(by creating a new one)
	var lastIndex int = rf.getLastIndex()
	var newLog []Log // If my current log is left behind, then delete all entries.
	if index < lastIndex {
		for i := index + 1; i <= lastIndex; i++ {
			newLog = append(newLog, rf.log[i-rf.lastIncludedIndex-1])
		}
	}

	// Update the state
	// In real life, the index may be larger than than current last index,
	// which may cause index out of range for getting the lastIncludedTerm,
	// so we need one more argument called lastIncludedTerm to update.
	// But here the test procedures ensure that the index won't be larger than
	// the current commit index.
	rf.lastIncludedTerm = rf.log[index-rf.lastIncludedIndex-1].Term
	rf.lastIncludedIndex = index
	rf.log = newLog
	rf.snapShot = snapshot

	//Apply to the state machine and persist.
	// rf.applyChan <- ApplyMsg{               NOTICE: When you send one snapshot to the applych, the test procedures will treat your current
	// 	CommandValid:  false,                  log content equal to the snapshot, which will lead to mismatch when your log have more entries
	// 	SnapshotValid: true,				   than the snapshot.
	// 	Snapshot:      snapshot,
	// 	SnapshotIndex: index + 1,
	// 	SnapshotTerm:  rf.lastIncludedTerm,
	// }

	go rf.persist(true)
}

// Leader sends snapshot to the stale follower.
func (rf *Raft) sendInstallSnapshot(server int) {
	rf.mu.Lock()
	DPrintf("WARNING: sendInstallSnapshot was called!!!!!")
	args := InstallSnapshotArgs{
		Term:              rf.currentTerm,
		LeaderId:          rf.me,
		LastIncludedIndex: rf.lastIncludedIndex,
		LastIncludedTerm:  rf.lastIncludedTerm,
		Data:              rf.snapShot,
	}
	reply := InstallSnapshotReply{}
	rf.mu.Unlock()

	if ok := rf.peers[server].Call("Raft.InstallSnapshot", &args, &reply); !ok {
		return
	}

	rf.mu.Lock()
	defer rf.mu.Unlock()
	// Not a valid leader, thus steps down.
	if reply.Term > rf.currentTerm {
		rf.currentTerm = reply.Term
		rf.rule = Follower
		rf.voteCount = 0
		rf.votedFor = -1
		rf.time.reset()
		go rf.persist(false)
	}
	rf.nextIndex[server] = rf.lastIncludedIndex + 1
}

func (rf *Raft) InstallSnapshot(args *InstallSnapshotArgs, reply *InstallSnapshotReply) {
	rf.mu.Lock()
	defer rf.mu.Unlock()
	// Not coming from a valid leader
	if args.Term < rf.currentTerm {
		reply.Term = rf.currentTerm
		return
	}

	// Update term
	rf.time.reset()
	if args.Term > rf.currentTerm {
		rf.currentTerm = args.Term
		rf.rule = Follower
		rf.voteCount = 0
		rf.votedFor = -1
	}
	reply.Term = rf.currentTerm

	// Update states
	var lastIndex int = rf.getLastIndex()
	var newLog []Log
	if args.LastIncludedIndex < lastIndex {
		for i := args.LastIncludedIndex + 1; i <= lastIndex; i++ {
			newLog = append(newLog, rf.log[i-rf.lastIncludedIndex-1])
		}
	}
	rf.log = newLog
	rf.snapShot = args.Data
	rf.commitIndex = args.LastIncludedIndex
	rf.lastIncludedIndex = args.LastIncludedIndex
	rf.lastIncludedTerm = args.LastIncludedTerm
	rf.lastApplied = rf.commitIndex

	applyMsg := &ApplyMsg{
		CommandValid:  false,
		SnapshotValid: true,
		Snapshot:      rf.snapShot,
		SnapshotIndex: rf.lastIncludedIndex + 1,
		SnapshotTerm:  rf.lastIncludedTerm,
	}
	// Apply to the state machine and persist.
	rf.applyChan <- *applyMsg
	go rf.persist(true)

}

// example RequestVote RPC arguments structure.
// field names must start with capital letters!
type RequestVoteArgs struct {
	// Your data here (2A, 2B).
	Term         int
	CandidateId  int
	LastLogIndex int
	LastLogTerm  int
}

// example RequestVote RPC reply structure.
// field names must start with capital letters!
type RequestVoteReply struct {
	// Your data here (2A).
	Term        int
	VoteGranted bool
}

type AppendEntriesArgs struct {
	Term         int
	LeaderId     int
	PrevLogIndex int
	PrevLogTerm  int
	LeaderCommit int
	Entries      []Log
}

type AppendEntriesReply struct {
	Term       int
	Success    int
	MatchIndex int
	XTerm      int
	XIndex     int
	Xlen       int
}

type InstallSnapshotArgs struct {
	Term              int
	LeaderId          int
	LastIncludedIndex int
	LastIncludedTerm  int
	Data              []byte
}

type InstallSnapshotReply struct {
	Term int
}

func (rf *Raft) applyLogs() {
	DPrintf("server %v commit entries before index %v", rf.me, rf.commitIndex)
	for i := rf.lastApplied + 1; i <= rf.commitIndex; i++ {
		rf.applyChan <- ApplyMsg{
			CommandValid: true,
			Command:      rf.log[i-rf.lastIncludedIndex-1].Command,
			CommandIndex: i + 1,
		}

	}
	rf.lastApplied = rf.commitIndex
}

func (rf *Raft) getLastIndex() int {
	return len(rf.log) + rf.lastIncludedIndex
}

func (rf *Raft) getLastTerm() int {
	if len(rf.log) == 0 {
		return rf.lastIncludedTerm
	}
	return rf.log[len(rf.log)-1].Term
}

// example RequestVote RPC handler.
func (rf *Raft) RequestVote(args *RequestVoteArgs, reply *RequestVoteReply) {
	// Your code here (2A, 2B).
	rf.mu.Lock()
	defer rf.mu.Unlock()
	// You cannot be a leader.
	if args.Term <= rf.currentTerm {
		reply.VoteGranted = false
		reply.Term = rf.currentTerm
		return
	}

	// Your term larger, I become a follower, but no guaranteed vote.
	rf.currentTerm = args.Term
	rf.votedFor = -1
	rf.voteCount = 0
	rf.rule = Follower

	// Check if your entries are at least up-to-date as mine.
	// Say no to the candidate.
	if args.LastLogTerm < rf.getLastTerm() || args.LastLogTerm == rf.getLastTerm() && args.LastLogIndex < rf.getLastIndex() {
		reply.VoteGranted = false
		reply.Term = rf.currentTerm
		go rf.persist(false)
		return
	}

	// Vote for the candidate.
	rf.votedFor = args.CandidateId
	reply.VoteGranted = true
	reply.Term = rf.currentTerm
	rf.time.reset()
	go rf.persist(false)
}

func (rf *Raft) sendAppendEntries(server int, args *AppendEntriesArgs, reply *AppendEntriesReply) {
	// out-of-date request
	rf.mu.Lock()
	if args.Term < rf.currentTerm || rf.rule != Leader {
		DPrintf("The sendAppendEntries request comes from server %v term %v, while current term is %v, right now my rule is %v", rf.me, args.Term, rf.currentTerm, rf.rule)
		rf.mu.Unlock()
		return
	}
	rf.mu.Unlock()

	DPrintf("Leader %v term %v sendAppendEntries to server %v", rf.me, rf.currentTerm, server)
	if ok := rf.peers[server].Call("Raft.AppendEntries", args, reply); !ok {
		DPrintf("Leader %v in term %v call server %v error!", rf.me, rf.currentTerm, server)
		return
	}

	rf.mu.Lock()
	defer rf.mu.Unlock()
	// I am not a valid leader, thus step down.
	if reply.Success == 10 {
		if rf.rule == Leader {
			DPrintf("I am %v, not a valid leader anymore, and my Leader term was %v", rf.me, rf.currentTerm)
			rf.rule = Follower
			rf.currentTerm = reply.Term
			rf.votedFor = -1
			rf.voteCount = 0
			go rf.persist(false)
			rf.time.reset()
		}
		return
	}

	if reply.Success == 2 {
		// Follower's log is too short.
		if reply.Xlen < rf.nextIndex[server] {
			DPrintf("Too short!, server %v next index should be %v, while reply.Xlen is %v", server, rf.nextIndex[server], reply.Xlen)
			rf.nextIndex[server] = reply.Xlen
			DPrintf("Now change server %v nextIndex to %v", server, rf.nextIndex[server])
			return
		}

		// Follower's log is long enough, but term does not match. Look for leader's last entry for XTerm.
		var i int
		for i = rf.nextIndex[server] - 1; i > rf.lastIncludedIndex; i-- {
			if rf.log[i-rf.lastIncludedIndex-1].Term == reply.XTerm {
				break
			}
		}
		if i > rf.lastIncludedIndex {
			// Matching term found, update rf.nextIndex[server].
			DPrintf("Matching term found, update server %v nextIndex %v.", server, i)
			rf.nextIndex[server] = i
		} else {
			// No matching term found.
			DPrintf("No matching term found, update server %v nextIndex %v.", server, reply.XIndex)
			rf.nextIndex[server] = reply.XIndex
		}
		return
	}

	DPrintf("Leader %v: Term: %v Match success, server %v matchIndex now update to %v", rf.me, rf.currentTerm, server, reply.MatchIndex)
	// Match. Update its follower's state
	rf.matchIndex[server] = reply.MatchIndex
	rf.nextIndex[server] = rf.matchIndex[server] + 1

	// Check if commit entries
	count := 1
	for i := 0; i < len(rf.peers); i++ {
		if i == rf.me {
			continue
		}
		if rf.matchIndex[i] >= rf.matchIndex[server] {
			count++
		}
	}

	if count > len(rf.peers)/2 && rf.matchIndex[server] > rf.commitIndex && rf.log[rf.matchIndex[server]-rf.lastIncludedIndex-1].Term == rf.currentTerm {
		rf.commitIndex = rf.matchIndex[server]
		DPrintf("Leader %v in term %v commitIndex becomes %v", rf.me, rf.currentTerm, rf.commitIndex)
		rf.applyLogs()
	}
}

func (rf *Raft) AppendEntries(args *AppendEntriesArgs, reply *AppendEntriesReply) {
	rf.mu.Lock()
	defer rf.mu.Unlock()
	// Respond to the unqualified leader.
	if args.Term < rf.currentTerm {
		reply.Term = rf.currentTerm
		reply.Success = 10
		DPrintf("server %v: You server %v are not a valid leader, your term is %v, while my term is %v", rf.me, args.LeaderId, args.Term, rf.currentTerm)
		return
	}

	// Convert to a Follower if not.
	rf.time.reset()
	rf.rule = Follower
	rf.votedFor = -1
	rf.voteCount = 0
	rf.currentTerm = args.Term
	reply.Term = rf.currentTerm
	DPrintf("server %v recieves valid AppendEntries rpc in term %v", rf.me, rf.currentTerm)

	lastIndex := rf.getLastIndex()
	// length of log is long enough
	if lastIndex >= args.PrevLogIndex {
		// but term does not match
		var term int
		if args.PrevLogIndex > rf.lastIncludedIndex {
			term = rf.log[args.PrevLogIndex-rf.lastIncludedIndex-1].Term
		} else {
			term = rf.lastIncludedTerm
		}
		if term != args.PrevLogTerm {
			i := args.PrevLogIndex - rf.lastIncludedIndex - 1
			for i >= 0 && rf.log[i].Term == term {
				i--
			}
			reply.XIndex = i + 1
			reply.Term = rf.currentTerm
			reply.Xlen = args.PrevLogIndex + 1
			reply.MatchIndex = -1
			reply.Success = 2
			reply.XTerm = term
			DPrintf("server %v term does not match, I reply index is %v", rf.me, i+1)
			return
		}
	} else { // log is too short
		reply.Xlen = len(rf.log) + rf.lastIncludedIndex + 1
		reply.MatchIndex = -1
		reply.Success = 2
		DPrintf("server %v: my len(log) is %v, too short!", rf.me, len(rf.log)+rf.lastIncludedIndex+1)
		return
	}

	// Now consistent, delete all entries after the consistent ones and append new ones.
	reply.Success = 1
	rf.log = rf.log[:args.PrevLogIndex-rf.lastIncludedIndex]
	rf.log = append(rf.log, args.Entries...)

	reply.MatchIndex = rf.getLastIndex()
	DPrintf("server %v reply.MatchIndex %v", rf.me, reply.MatchIndex)

	// Update commit index and apply.
	if args.LeaderCommit > rf.commitIndex {
		old := rf.commitIndex
		rf.commitIndex = min(args.LeaderCommit, rf.getLastIndex())
		if rf.commitIndex > old {
			DPrintf("server %v commit index becomes %v", rf.me, rf.commitIndex)
			rf.applyLogs()
		}
	}

	go rf.persist(false)
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
func (rf *Raft) sendRequestVote(server int, args *RequestVoteArgs, reply *RequestVoteReply) {
	DPrintf("server %v sendRequestVote to server %v", rf.me, server)
	if ok := rf.peers[server].Call("Raft.RequestVote", args, reply); !ok {
		DPrintf("server %v sendRequestVote to server %v error", rf.me, server)
		return
	}
	rf.mu.Lock()
	defer rf.mu.Unlock()
	// I am not a valid leader anymore.
	if reply.Term > rf.currentTerm {
		rf.rule = Follower
		rf.votedFor = -1
		rf.voteCount = 0
		rf.currentTerm = reply.Term
		go rf.persist(false)
		rf.time.reset()
		return
	}

	if rf.rule == Leader {
		return
	}

	if reply.VoteGranted {
		DPrintf("server %v recieves vote from server %v", rf.me, server)
		rf.voteCount++
	}

	if rf.voteCount > len(rf.peers)/2 {
		rf.rule = Leader
		DPrintf("server %v becomes Leader! Term: %v", rf.me, rf.currentTerm)
		for i := 0; i < len(rf.peers); i++ {
			if i == rf.me {
				continue
			}
			rf.nextIndex[i] = rf.getLastIndex() + 1
			rf.matchIndex[i] = -1
		}
	}
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

	// Your code here (2B).
	rf.mu.Lock()
	defer rf.mu.Unlock()
	if rf.rule != Leader {
		return index, term, false
	}

	term = rf.currentTerm
	rf.log = append(rf.log, Log{Term: rf.currentTerm, Command: command})
	index = len(rf.log) + rf.lastIncludedIndex + 1 // Notice!
	go rf.persist(false)
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

func (rf *Raft) leaderElection() {
	rf.mu.Lock()
	defer rf.mu.Unlock()
	rf.rule = Candidate
	rf.currentTerm++
	rf.voteCount = 1
	rf.votedFor = rf.me
	go rf.persist(false)
	rf.time.reset()

	for i := 0; i < len(rf.peers); i++ {
		if i == rf.me {
			continue
		}
		args := RequestVoteArgs{Term: rf.currentTerm, CandidateId: rf.me}
		args.LastLogIndex = rf.getLastIndex()
		args.LastLogTerm = rf.getLastTerm()
		reply := RequestVoteReply{}

		go rf.sendRequestVote(i, &args, &reply)
	}
}

func (rf *Raft) heartBeat() {

	for i := 0; i < len(rf.peers); i++ {
		if i == rf.me {
			continue
		}
		rf.mu.Lock()
		// Give up sending "AppendEntries", otherwise sending snapshots.
		if rf.nextIndex[i] <= rf.lastIncludedIndex {
			rf.mu.Unlock()
			go rf.sendInstallSnapshot(i)
			continue
		}

		// Not too far behind
		args := AppendEntriesArgs{Term: rf.currentTerm, LeaderId: rf.me, LeaderCommit: rf.commitIndex}
		reply := AppendEntriesReply{}
		args.PrevLogIndex = rf.nextIndex[i] - 1
		DPrintf("Leader %v in term %v heartBeat: server %v your prev index is %v", rf.me, rf.currentTerm, i, args.PrevLogIndex)
		if args.PrevLogIndex == rf.lastIncludedIndex {
			args.PrevLogTerm = rf.lastIncludedTerm
		} else {
			args.PrevLogTerm = rf.log[args.PrevLogIndex-rf.lastIncludedIndex-1].Term
		}

		if rf.getLastIndex() >= rf.nextIndex[i] {
			args.Entries = rf.log[rf.nextIndex[i]-rf.lastIncludedIndex-1:]
		} else {
			args.Entries = []Log{}
		}
		rf.mu.Unlock()
		go rf.sendAppendEntries(i, &args, &reply)
	}
}

func (rf *Raft) ticker() {
	for !rf.killed() {
		// Your code here (2A)
		// Check if a leader election should be started.
		rf.mu.Lock()

		if rf.rule == Leader {
			rf.mu.Unlock()
			rf.heartBeat()
			time.Sleep(30 * time.Millisecond)
		} else {
			if time.Since(rf.time.initTime) > rf.time.duration {
				rf.mu.Unlock()
				rf.leaderElection()
			} else {
				rf.mu.Unlock()
			}
			ms := 30 + (rand.Int63() % 30)
			time.Sleep(time.Duration(ms) * time.Millisecond)
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
	persister *Persister, applyCh chan ApplyMsg) *Raft {
	DPrintf("Make called on server %v", me)
	rf := &Raft{}
	rf.peers = peers
	rf.persister = persister
	rf.me = me

	// Your initialization code here (2A, 2B, 2C).
	rf.votedFor = -1
	rf.rule = Follower
	rf.log = make([]Log, 0)
	rf.currentTerm = 0
	rf.commitIndex = -1
	rf.lastApplied = -1
	rf.nextIndex = make([]int, len(peers))
	rf.matchIndex = make([]int, len(peers))
	rf.applyChan = applyCh
	rf.lastIncludedIndex = -1
	rf.lastIncludedTerm = -1
	rf.time = &Time{}
	rf.time.reset()

	// initialize from state persisted before a crash
	rf.readPersist(persister.ReadRaftState())

	// start ticker goroutine to start elections
	go rf.ticker()

	return rf
}
