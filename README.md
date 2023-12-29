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

在进行这个LAB时，你可能遇到低级错误如：死锁、数组超出索引（逻辑错误导致）、抑或是没注意到论文中的细节导致没有达成一致、抑或是没有“迎合”测试程序，而导致测试不通过。我这里有几条Debug经验之谈：

* __Dprintf__:这是MIT推荐的“笨办法”，但也是最直截了当最快定位问题所在的办法！在你怀疑的地方打印尽可能详细的信息。
* 不要放过论文中的细节，有时候论文中的细节还不够，可以去官网动画自定义case来解决你的疑惑！
* 当你以为抓住了所有细节也没发现问题，那就可以去测试程序那里找答案，虽然阅读大量的测试程序非常折磨。。。但正是这样我才发现了我在应用快照导致的死锁问题是测试程序和我共同产生的：我在SnapShot函数里向channel发送快照，但是同时测试程序里需要在调用SnapShot返回后才会从channel里接受信息。要么我修改测试程序，在SnapShot调用前加go，要么我不在这个函数里应用快照。所以如果你不知道测试函数的具体应用，即使你的逻辑正确，也可能会过不了测试。

