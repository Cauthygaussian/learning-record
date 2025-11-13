// author:xsh
package mr

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"time"
)
import "log"
import "net/rpc"
import "hash/fnv"


//
// Map functions return a slice of KeyValue.
//
type KeyValue struct {
	Key   string
	Value string
}


//
// use ihash(key) % NReduce to choose the reduce
// task number for each KeyValue emitted by Map.
//
func ihash(key string) int {
	h := fnv.New32a()
	h.Write([]byte(key))
	return int(h.Sum32() & 0x7fffffff)
}


func generateFileName(r int, NMap int) []string {
    // 为 reduce r 生成所有 map 任务产生的中间文件名：mr-m-r
    if NMap <= 0 {
        return []string{}
    }
    filenames := make([]string, 0, NMap)
    for i := 0; i < NMap; i++ {
        filenames = append(filenames, fmt.Sprintf("mr-%v-%v", i, r))
    }
    return filenames
}


//
// main/mrworker.go calls this function.
//
func Worker(mapf func(string, string) []KeyValue,
	reducef func(string, []string) string) {

	// Your worker implementation here.
	for{
		args := MessageSend{}
		reply := MessageReply{}

		ok := call("Coordinator.RequestTask", &args, &reply)
		if !ok {
			// 协调器未就绪/连接失败：静默等待后重试，避免噪声日志。
			time.Sleep(10 * time.Millisecond)
			continue
		}

		switch(reply.TaskType){
			case MapTask:HandleMapTask(mapf, reply)
		case ReduceTask:HandleReduceTask(reducef, reply) 
		case Wait:time.Sleep(time.Second * 10)
		case Exit:os.Exit(0)
		default: time.Sleep(time.Second * 10)
		}
		
	}

	// uncomment to send the Example RPC to the coordinator.
	// CallExample()

}

func HandleMapTask(mapf func(string, string) []KeyValue, reply MessageReply){
	filename := reply.TaskFile

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

	intermediate := make([][]KeyValue, reply.NReduce)

	for _, kv := range kva{
		index := ihash(kv.Key) % reply.NReduce
		intermediate[index] = append(intermediate[index], kv)
	}

	for r, kva := range intermediate{
		oname := fmt.Sprintf("mr-%v-%v", reply.TaskID, r)
		ofile, err := os.CreateTemp("", oname)
		if err != nil{
			log.Fatalf("cannot create temp file %v", oname)
		}

		enc := json.NewEncoder(ofile)
		for _, kv := range kva{
			enc.Encode(kv)
		}

		ofile.Close()
		os.Rename(ofile.Name(), oname)
	}

	args := MessageSend{
		TaskID: reply.TaskID,
		TaskStatus: MapTaskCompleted,
	}

	call("Coordinator.ReportTask", &args, &MessageReply{})
}

func HandleReduceTask(reducef func(string, []string) string, reply MessageReply){
	intermediate := []KeyValue {}

	intermediateFiles := generateFileName(reply.TaskID, reply.NMap)

	for _, filename := range intermediateFiles{
		file, err := os.Open(filename)
		if err != nil {
			log.Fatalf("cannot open %v", filename)
		}
		dec := json.NewDecoder(file)
		for {
			kv := KeyValue{}
			if err := dec.Decode(&kv); err == io.EOF{
				break
			}
			intermediate = append(intermediate, kv)
		}
		file.Close()
	}

	sort.Slice(intermediate, func(i,j int)bool{
		return intermediate[i].Key < intermediate[j].Key
	})

	oname := fmt.Sprintf("mr-out-%v", reply.TaskID)

	ofile, err := os.Create(oname)
	if err != nil{
		log.Fatalf("cannot create output file %v", oname)
	}

	for i := 0; i < len(intermediate); {
		j := i + 1
		for j < len(intermediate) && intermediate[j].Key == intermediate[i].Key {
			j++
		}
		values := []string{}
		for k := i; k < j; k++ {
			values = append(values, intermediate[k].Value)
		}
		output := reducef(intermediate[i].Key, values)

		// this is the correct format for each line of Reduce output.
		fmt.Fprintf(ofile, "%v %v\n", intermediate[i].Key, output)

		i = j
	}

	ofile.Close()
	os.Rename(ofile.Name(), oname)

	args := MessageSend{
		TaskID: reply.TaskID,
		TaskStatus: ReduceTaskCompleted,
	}
	call("Coordinator.ReportTask", &args, &MessageReply{})
}

//
// example function to show how to make an RPC call to the coordinator.
//
// the RPC argument and reply types are defined in rpc.go.
//

//
// send an RPC request to the coordinator, wait for the response.
// usually returns true.
// returns false if something goes wrong.
//
func call(rpcname string, args interface{}, reply interface{}) bool {
	// c, err := rpc.DialHTTP("tcp", "127.0.0.1"+":1234")
	sockname := coordinatorSock()
	c, err := rpc.DialHTTP("unix", sockname)
	if err != nil {
		// 避免打印致命日志（带时间戳），返回 false 让上层自行重试。
		return false
	}
	defer c.Close()

	err = c.Call(rpcname, args, reply)
	if err == nil {
		return true
	}

	fmt.Println(err)
	return false
}
