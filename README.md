# MIT6.5840
2023Spring课程链接https://pdos.csail.mit.edu/6.824/schedule.html

Raft官网的可交互性动画对于理解论文中的细节非常有帮助https://raft.github.io

# LAB2: Raft
Raft是分布式系统中理解起来相对容易的一致性算法/协议。一致性对于fault-tolerant systems非常重要，Raft通过几个重要的特性来实现一致性（论文Figure 3）：
*  __Election Safety__: at most one leader can be elected in a given term.
*  __Leader Append-Only__: a leader never overwrites or deletes entries in its log; it only appends new entries.
*  __Log Matching__: if two logs contain an entry with the same index and term, then the logs are identical in all entries up through the given index.
*  __Leader Completeness__: if a log entry is committed in a given term, then that entry will be present in the logs of the leaders for all higher-numbered terms.
*  __State Machine Safety__: if a server has applied a log entry at a given index to its state machine, no other server will ever apply a different log entry for the same index.

换言之：
* 某一任期只有一个Leader。
* Leader不会删除自己的日志条目，只会增加。
* 当两个日志中的某条目索引和该条目的任期匹配，我们才说这两个日志匹配。
* 如果某一个日志的条目被提交了，那么后续的领导者一定有这一部分条目（条目需要大多数机器拥有才可以提交 + 拥有最新条目的Raft才可以被选举为Leader）。
* 同一个索引的条目不可以被多次提交。

本LAB中无论Raft是Follower，Leader，还是Candidate，它们之间都通过RPC来交换信息。
大概流程是这样的：

服务层调用Make()来创建一个个Raft peer，创建完成之后，会时不时的向各个Raft发送command信息（通过调用Start(command)），当然只有Leader才会应答。在这个过程中我们需要顺利完成选举过程，并且Leader需要顺利地将日志条目发送给其他机器。当大部分机器都收到该条目后，Leader会在下一次的RPC中通过commitIndex来告知其他选手我已经把这之前的信息都应用到状态机，其他选手才可以应用到状态机。本LAB通过模拟将应用的信息通过apply channel发送给测试程序来模拟这个应用的过程。想要通过所有测试程序，需要考虑到所有苛刻的条件：例如某个Raft突然崩溃，网络不稳定造成疯狂丢包，而且有些测试程序需要你在规定时间完成，因此需要加速某一部分代码使得Follower尽快赶上Leader的进度... 在后续的日志持久化和应用快照部分会将你本可以通过2A、2B测试的代码存在的问题都暴露出来。

想要完成本LAB，一个Raft Structure大概如下：
```go
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

```

在进行这个LAB时，你可能遇到低级错误如：死锁、数组超出索引（逻辑错误导致）、抑或是没注意到论文中的细节导致没有达成一致、抑或是没有“迎合”测试程序，而导致测试不通过。我这里有几条Debug经验之谈：

* __Dprintf__:这是MIT推荐的“笨办法”，但也是最直截了当最快定位问题所在的办法！在你怀疑的地方打印尽可能详细的信息。
* 不要放过论文中的细节，有时候论文中的细节还不够，可以去官网动画自定义case来解决你的疑惑！
* 当你以为抓住了所有细节也没发现问题，那就可以去测试程序那里找答案，虽然阅读大量的测试程序非常折磨。。。但正是这样我才发现了我在应用快照导致的死锁问题是测试程序和我共同产生的：我在SnapShot函数里向channel发送快照，但是同时测试程序里需要在调用SnapShot返回后才会从channel里接受信息。要么我修改测试程序，在SnapShot调用前加go，要么我不在这个函数里应用快照。所以如果你不知道测试函数的具体应用，即使你的逻辑正确，也可能会过不了测试。

# 2A: Leader election
选举Leader的大致流程就是某一个Follower等待Leader的信息超时，变身为Candidate，然后向其他人request votes，如果获得超半数的选票便成为Leader，向其他人发送AppendEntries命令（并不是真的有日志条目）。如何才能获得他人的选票和如何才能保持Leader身份论文中有详细描述，有些模糊的细节你可以在官网动画中私人定制来获取😎。注意发送RPC频率不能太快不然会过不了RPCs aren't too high的测试。

# 2B: Log
这一部分是Raft中的核心部分，上层service通过Start函数向各个Raft发送command,Leader会正确回应这个条目将会出现的Index。我一直觉得这一部分的回复无关紧要，后来才知道测试程序会检验你的Apply channel中的index是否与你回复Start的index一致。另外重要的一点，回复给服务层的索引是从1开始的，所以在写apply函数的时候注意这一点就可以了。

另外需要注意的是，如果Leader的选举过程过慢或者频繁触发选举过程会导致后面的测试难以通过。要把握好Follower的超时时间和Leader发送HeartBeat的时间。Lab官网对这一部分也有解释。

# 2C: Persistence
机器崩溃时会从readPersist中加载它崩溃前持久化的各个参数。只需要在每个修改持久化参数的地方调用persist即可。

NOTICE: 这一部分需要对Leader和Follower之间的通信进行加速，否则长时间不能达成一致会过不了测试。如果Leader发送的日志条目的索引和任期与Follower不符，我2B的解决方案是通过RPC call一次次递减index，直到Leader和Follower达成一致，但是如果Log中的日志条目过多的时候这样会很慢。因为在本Lab中，测试程序会模拟网络丢包过程！有一定几率会将Request和Reply信息直接丢掉（相关代码应该是在labrpc中），或者某台机器崩溃，或者将某台机器断网 —— 例如收不到任何消息却仍然以为自己是Leader不停发送AppendEntries的信息。所以快速达成一致是防止发生以上意外情况导致测试失败的关键。

课程官网给出的建议是在Follower回复的消息中增加关键信息：
* XTerm:  term in the conflicting entry (if any)
* XIndex: index of first entry with that term (if any)
* XLen:   log length

Leader处理流程如下：
* Case 1: leader doesn't have XTerm:
    nextIndex = XIndex
* Case 2: leader has XTerm:
    nextIndex = leader's last entry for XTerm
* Case 3: follower's log is too short:
    nextIndex = XLen

当然你可以有自己的加速方式，如果对某种情况不理解应该怎么处理，直接去官网动画中定制的你的疑惑即可。

# 2D: Log compaction
随着Log的不断扩大，不断占用内存空间，我们需要删除旧的Log，而将旧的部分信息保存在snapshot中。如果一个Leader发现某一个Follower太过落后以至于现存的Log无法将它的信息补全，那么就会向它发送快照。此外，service也会向各个Raft发送snapshot，表示某些信息已经过时了，你可以删掉了。这两部分都可以确保一个Follower快速跟上Leader的步伐。

那么service的snapshot从哪里来的呢，查看测试程序可以发现，当日志条目达到某一个数量时，他会直接将你的Log entries编码然后调用SnapShot，这是在接受raft apply信息的过程中顺便做的。也正是如此，我们不应该在SnapShot中将快照应用到状态机（逻辑上本该是这样的），因为channel这边你在发送快照，channel那面需要等待SnapShot函数执行完后才从channel中接受数据，因此在SnapShot发送快照会导致Dead Lock!如果非要发送，将测试程序调用该函数前加一个'go'就可以了。

我在做这一部分的时候，除了遇到上述死锁问题，测试程序告诉我Failed to reach agreement!我百思不得其解，终于发现从测试程序中发现端倪。

当Raft崩溃重启时，我之前的理解是，lastCommitIndex也就是最后提交的index应该是读取快照的lastIncluded Index，也就是快照的最后一个index。因为既然我们已经持久化了快照信息，为何不利用快照信息而获得尽可能最新的信息呢。但是测试程序偏不，他将所有的lastCommitedIndex都设置成0，也就是程序索引中的-1。也就是崩溃重启时，每个Raft最后提交的索引直接重置好似为什么事没发生过😡。在没有持久化快照信息前的2B/2C部分，这样做非常有道理，但是有了快照为何不用呢？当我发现这个问题时已经debug到几乎失去耐心，于是我直接将测试程序中的重启代码稍微修改了一下（过不了就修改他🤣），将他重启的lastCommitIndex设置成我认为的lastIncluded Index+1而不是0。所以如果你持久化了快照相关信息，请不要在lastCommitIndex上做功夫，否则会过不掉测试。

所以有时候过不掉测试的原因并不是你的逻辑错误，而是你和测试程序的逻辑不相符。遇到这种问题就需要花费大量时间，排出前面所有情况，并尝试去理解测试程序的逻辑。
