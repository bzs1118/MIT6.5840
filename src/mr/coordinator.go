package mr

import (
	"log"
	"net"
	"net/http"
	"net/rpc"
	"os"
	"sync"
	"time"
)

const Debug = false

func DPrintln(a ...interface{}) (n int, err error) {
	if Debug {
		log.Println(a...)
	}
	return
}

func DPrintf(format string, a ...interface{}) (n int, err error) {
	if Debug {
		log.Printf(format, a...)
	}
	return
}

const (
	MAP       = "map"
	REDUCE    = "reduce"
	WAITING   = "waiting"
	QUIT      = "quit"
	FINISHIED = "finishied"
	TIME_OUT  = 10 * time.Second
)

type Coordinator struct {
	// Your definitions here.
	state         string
	nReduce       int
	nMap          int
	taskQue       chan *Task
	finishedTasks int
	onGoingTask   map[int]*Task
	mu            sync.Mutex
}

type Task struct {
	ID       int
	Type     string
	FileName string
	NReduce  int
	NMap     int
	Deadline int64
}

// Your code here -- RPC handlers for the worker to call.

// an example RPC handler.
//
// the RPC argument and reply types are defined in rpc.go.
func (c *Coordinator) Example(args *ExampleArgs, reply *ExampleReply) error {
	reply.Y = args.X + 1
	return nil
}

// start a thread that listens for RPCs from worker.go
func (c *Coordinator) server() {
	rpc.Register(c)
	rpc.HandleHTTP()
	//l, e := net.Listen("tcp", ":1234")
	sockname := coordinatorSock()
	os.Remove(sockname)
	l, e := net.Listen("unix", sockname)
	if e != nil {
		log.Fatal("listen error:", e)
	}
	go http.Serve(l, nil)
}

// main/mrcoordinator.go calls Done() periodically to find out
// if the entire job has finished.
var ret bool

func (c *Coordinator) Done() bool {
	// Your code here.
	c.mu.Lock()
	defer c.mu.Unlock()
	ret = c.state == FINISHIED
	return ret
}

// create a Coordinator.
// main/mrcoordinator.go calls this function.
// nReduce is the number of reduce tasks to use.
func MakeCoordinator(files []string, nReduce int) *Coordinator {
	c := Coordinator{}

	// Your code here.
	c.nReduce = nReduce
	c.nMap = len(files)
	c.taskQue = make(chan *Task, max(len(files), nReduce))
	c.mu = sync.Mutex{}
	c.state = MAP
	c.onGoingTask = make(map[int]*Task)
	c.finishedTasks = 0

	for i, filename := range files {
		// Create a map task.
		task := Task{
			ID:       i,
			Type:     MAP,
			FileName: filename,
			NReduce:  c.nReduce,
			NMap:     c.nMap,
			Deadline: -1,
		}
		c.taskQue <- &task
	}

	go c.detector()
	go c.server()
	return &c
}

func (c *Coordinator) detector() {
	for {
		c.mu.Lock()
		if c.state == MAP && c.finishedTasks == c.nMap ||
			c.state == REDUCE && c.finishedTasks == c.nReduce ||
			c.state == QUIT && c.finishedTasks == c.nReduce {
			c.changeState()
		} else {
			c.taskTimeout()
		}
		c.mu.Unlock()
		time.Sleep(200 * time.Millisecond)
	}
}

func (c *Coordinator) taskTimeout() {
	// Check if there are any timeouts in the currently running tasks,
	// remove them from c.onGongingTask, and then re-add them to c.taskQue.
	tasksToDelete := []int{}
	for taskID, task := range c.onGoingTask {
		if time.Now().Unix() > task.Deadline {
			task.Deadline = -1
			c.taskQue <- task
			tasksToDelete = append(tasksToDelete, taskID)
		}
	}

	for _, key := range tasksToDelete {
		delete(c.onGoingTask, key)
	}
}

func (c *Coordinator) changeState() {
	if c.state == MAP {
		// Shift current state to Reduce
		c.state = REDUCE
		for i := 0; i < c.nReduce; i++ {
			// Create a reduce task.
			task := Task{
				ID:       i,
				Type:     REDUCE,
				NReduce:  c.nReduce,
				NMap:     c.nMap,
				Deadline: -1,
			}
			c.taskQue <- &task
			c.finishedTasks = 0
		}
	} else if c.state == REDUCE {
		DPrintln("QUIT!!!!")
		c.state = QUIT
	} else {
		DPrintln("FINISHED!!!!")
		c.state = FINISHIED
		os.Exit(0)
	}
}

func (c *Coordinator) RequestTask(args *RequestTaskArgs, reply *RequestTaskReply) error {
	c.mu.Lock()
	if len(c.taskQue) != 0 {
		task := <-c.taskQue
		task.Deadline = time.Now().Add(TIME_OUT).Unix()
		reply.Task = task
		c.onGoingTask[task.ID] = task
	} else if len(c.taskQue) == 0 && c.state != QUIT {
		reply.Task = &Task{Type: WAITING}
	} else {
		reply.Task = &Task{Type: QUIT}
	}
	c.mu.Unlock()
	return nil
}

func (c *Coordinator) TaskDone(args *TaskDoneArgs, reply *TaskDoneReply) error {
	c.mu.Lock()
	delete(c.onGoingTask, args.ID)
	c.finishedTasks++
	c.mu.Unlock()
	return nil
}
