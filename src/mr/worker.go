package mr

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io"
	"log"
	"net/rpc"
	"os"
	"sort"

	"time"
)

// Map functions return a slice of KeyValue.
type KeyValue struct {
	Key   string
	Value string
}

type ByKey []KeyValue

// for sorting by key.
func (a ByKey) Len() int           { return len(a) }
func (a ByKey) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a ByKey) Less(i, j int) bool { return a[i].Key < a[j].Key }

// use ihash(key) % NReduce to choose the reduce
// task number for each KeyValue emitted by Map.
func ihash(key string) int {
	h := fnv.New32a()
	h.Write([]byte(key))
	return int(h.Sum32() & 0x7fffffff)
}

// main/mrworker.go calls this function.
func Worker(mapf func(string, string) []KeyValue,
	reducef func(string, []string) string) {

	// Your worker implementation here.
	for {

		task := RequestTask()

		switch task.Type {
		case MAP:
			MapTask(task, mapf)

		case REDUCE:
			ReduceTask(task, reducef)

		case WAITING:
			DPrintln("Waitng...")
			time.Sleep(100 * time.Millisecond)

		case QUIT:
			os.Exit(0)
		}

		time.Sleep(200 * time.Millisecond)
	}
}

func RequestTask() *Task {
	args := RequestTaskArgs{}
	reply := RequestTaskReply{}

	if ok := call("Coordinator.RequestTask", &args, &reply); !ok {
		log.Println("RequestTask call failed!")
	}
	return reply.Task
}

func sendTaskDone(taskId int, taskType string) {
	args := TaskDoneArgs{taskId, taskType}
	reply := TaskDoneReply{}

	if ok := call("Coordinator.TaskDone", &args, &reply); !ok {
		log.Println("sendTaskDone failed!")
	}
}

func MapTask(task *Task, mapf func(string, string) []KeyValue) {
	// Open the input file.
	filename := task.FileName

	file, err := os.Open(filename)
	if err != nil {
		log.Fatalf("cannot open %v", filename)
	}
	content, err := io.ReadAll(file)
	if err != nil {
		log.Fatalf("cannot read %v", filename)
	}
	file.Close()

	kva := mapf(filename, string(content))

	// Map a key to its corresponding reducer, and write it to a bucket (partitioning).
	buckets := make(map[int][]KeyValue)
	nReduce := task.NReduce
	for _, kv := range kva {
		index := ihash(kv.Key) % nReduce
		buckets[index] = append(buckets[index], kv)
	}

	// Prepare to keep track of temp files and their intended final names.
	tempFiles := make(map[string]string)

	// Write each bucket into a separate intermediate file.
	for i := 0; i < nReduce; i++ {
		// Create a temporary file
		tempFile, err := os.CreateTemp(".", "temp_map_")
		if err != nil {
			log.Fatalf("Failed to create temporary file: %v", err)
		}
		tempFileName := tempFile.Name()
		finalFileName := getIntermediateFileName(task.ID, i)
		tempFiles[tempFileName] = finalFileName

		// Write key/value pairs in JSON format to the temporary file
		enc := json.NewEncoder(tempFile)
		kvs := buckets[i]
		for _, kv := range kvs {
			if err := enc.Encode(&kv); err != nil {
				log.Fatalf("Fail to encode kv: %v", err)
			}
		}
		// Close the temporary file
		tempFile.Close()
	}

	// Rename all temporary files to their final names
	for tempFileName, finalFileName := range tempFiles {
		if err := os.Rename(tempFileName, finalFileName); err != nil {
			log.Fatalf("Failed to rename file from %s to %s: %v", tempFileName, finalFileName, err)
		}
	}

	go sendTaskDone(task.ID, task.Type)
}

func ReduceTask(task *Task, reducef func(string, []string) string) {
	nMap := task.NMap
	intermediate := make([]KeyValue, 0)
	// Read corresponding key-value pairs from mappers' outputs.
	for i := 0; i < nMap; i++ {
		rFile, err := os.Open(getIntermediateFileName(i, task.ID))
		if err != nil {
			log.Println("Open intermediate file failed!")
		}
		// Decode the file.
		dec := json.NewDecoder(rFile)
		for {
			var kv KeyValue
			if err := dec.Decode(&kv); err != nil {
				break
			}
			intermediate = append(intermediate, kv)
		}
	}
	// Sorting
	sort.Sort(ByKey(intermediate))
	tempFile, _ := os.CreateTemp(".", "temp_reduce_")
	i := 0
	for i < len(intermediate) {
		j := i + 1
		for j < len(intermediate) && intermediate[j].Key == intermediate[i].Key {
			j++
		}
		values := []string{}
		for k := i; k < j; k++ {
			values = append(values, intermediate[k].Value)
		}
		output := reducef(intermediate[i].Key, values)

		// This is the correct format for each line of Reduce output.
		fmt.Fprintf(tempFile, "%v %v\n", intermediate[i].Key, output)

		i = j
	}

	tempFileName := tempFile.Name()
	tempFile.Close()

	for i := 0; i < nMap; i++ {
		os.Remove(getIntermediateFileName(i, task.ID))
	}

	finalFileName := getOutputFileName(task.ID)
	if err := os.Rename(tempFileName, finalFileName); err != nil {
		log.Fatalf("Failed to rename file from %s to %s: %v", tempFileName, finalFileName, err)
	}
	DPrintln(task.ID, task.Type)
	go sendTaskDone(task.ID, task.Type)
}

func getIntermediateFileName(mapId int, reduceId int) string {
	return fmt.Sprintf("mr-%d-%d", mapId, reduceId)
}

func getOutputFileName(reduceNumber int) string {
	return fmt.Sprintf("mr-out-%d", reduceNumber)
}

// example function to show how to make an RPC call to the coordinator.
//
// the RPC argument and reply types are defined in rpc.go.
func CallExample() {

	// declare an argument structure.
	args := ExampleArgs{}

	// fill in the argument(s).
	args.X = 99

	// declare a reply structure.
	reply := ExampleReply{}

	// send the RPC request, wait for the reply.
	// the "Coordinator.Example" tells the
	// receiving server that we'd like to call
	// the Example() method of struct Coordinator.
	ok := call("Coordinator.Example", &args, &reply)
	if ok {
		// reply.Y should be 100.
		fmt.Printf("reply.Y %v\n", reply.Y)
	} else {
		fmt.Printf("call failed!\n")
	}
}

// send an RPC request to the coordinator, wait for the response.
// usually returns true.
// returns false if something goes wrong.
func call(rpcname string, args interface{}, reply interface{}) bool {
	// c, err := rpc.DialHTTP("tcp", "127.0.0.1"+":1234")
	sockname := coordinatorSock()
	c, err := rpc.DialHTTP("unix", sockname)
	if err != nil {
		log.Fatal("dialing:", err)
	}
	defer c.Close()
	err = c.Call(rpcname, args, reply)
	if err == nil {
		return true
	}

	fmt.Println(err)
	return false
}
