package mr


/*
注释，用于检验，看提交结果。
*/

/*
Git 分支推送速查（本文件仅附带说明，不影响运行）:
1. 查看/创建本地分支:
   git branch
   git switch -c mit6.824lab1   # 若分支不存在
2. 添加远程(若尚未添加):
   git remote add origin https://github.com/Cauthygaussian/learning-record.git
3. 获取远程:
   git fetch origin
4. 可选：与远程主分支同步:
   git pull --rebase origin main   # 或 master
5. 提交更新:
   git add .
   git commit -m "Update mit6.824lab1"
6. 首次推送并建立跟踪:
   git push -u origin mit6.824lab1
7. 后续更新:
   git push origin mit6.824lab1
8. 若提示 non-fast-forward 且确认覆盖:
   git push -f origin mit6.824lab1   # 谨慎
9. 查看远程分支:
   git ls-remote --heads origin
10. 推送成功后即可在 GitHub 上发起 PR 或直接使用该分支.
*/

// ...existing code...
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

// Map functions return a slice of KeyValue.
type KeyValue struct {
	Key   string
	Value string
}

// use ihash(key) % NReduce to choose the reduce
// task number for each KeyValue emitted by Map.
func ihash(key string) int {
	h := fnv.New32a()
	h.Write([]byte(key))
	return int(h.Sum32() & 0x7fffffff)
}

func generateFileName(r int, NMap int) []string {
	var fileName []string
	for taskid := 0; taskid < NMap; taskid++ {
		fileName = append(fileName, fmt.Sprintf("mr-%d-%d", taskid, r))
	}
	return fileName
}

// main/mrworker.go calls this function.
func Worker(mapf func(string, string) []KeyValue,
	reducef func(string, []string) string) {

	// Your worker implementation here.
	for {
		args := MessageSend{}
		reply := MessageReply{}

		ok := call("Coordinator.RequestTask", &args, &reply)
		if !ok {
			fmt.Printf("call failed!\n")
		}

		switch reply.TaskType {
		case MapTask:
			HandleMapTask(&reply, mapf)
		case ReduceTask:
			HandleReduceTask(&reply, reducef)
		case Wait:
			time.Sleep(time.Second)
		case Exit:
			os.Exit(0)
		default:
			time.Sleep(time.Second)
		}

	}
}

func HandleMapTask(reply *MessageReply, mapf func(string, string) []KeyValue) {
	file, err := os.Open(reply.TaskFile)
	if err != nil {
		log.Fatalf("cannot open %v", reply.TaskFile)
		return
	}

	content, err := io.ReadAll(file)
	if err != nil {
		log.Fatalf("cannot open %v", reply.TaskFile)
		return
	}
	file.Close()

	kva := mapf(reply.TaskFile, string(content))

	intermediate := make([][]KeyValue, reply.NReduce)

	for _, kv := range kva {
		r := ihash(kv.Key) % reply.NReduce
		intermediate[r] = append(intermediate[r], kv)
	}

	for r, kva := range intermediate {
		oname := fmt.Sprintf("mr-%v-%v", reply.TaskID, r)
		ofile, err := os.CreateTemp("", oname)
		if err != nil {
			log.Fatalf("cannot create tempfile %v", oname)
		}

		enc := json.NewEncoder(ofile)
		for _, kv := range kva {
			enc.Encode(kv)
		}

		ofile.Close()

		os.Rename(ofile.Name(), oname)
	}

	args := MessageSend{
		TaskID:              reply.TaskID,
		TaskCompletedStatus: MapTaskCompleted,
	}
	call("Coordinator.ReportTask", &args, &MessageReply{})
}

func HandleReduceTask(reply *MessageReply, reducef func(string, []string) string) {
	var intermediate []KeyValue

	intermediateFiles := generateFileName(reply.TaskID, reply.NMap)

	for _, filename := range intermediateFiles {
		file, err := os.Open(filename)
		if err != nil {
			log.Fatalf("cannot open %v", filename)
			return
		}

		dec := json.NewDecoder(file)
		for {
			kv := KeyValue{}
			if err := dec.Decode(&kv); err == io.EOF {
				break
			}
			intermediate = append(intermediate, kv)
		}
		file.Close()
	}

	sort.Slice(intermediate, func(i, j int) bool {
		return intermediate[i].Key < intermediate[j].Key
	})

	oname := fmt.Sprintf("mr-out-%v", reply.TaskID)
	ofile, err := os.Create(oname)

	if err != nil {
		log.Fatalf("cannot create %v", oname)
		return
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
		TaskID:              reply.TaskID,
		TaskCompletedStatus: ReduceTaskCompleted,
	}
	call("Coordinator.ReportTask", &args, &MessageReply{})
}

//
// example function to show how to make an RPC call to the coordinator.
//
// the RPC argument and reply types are defined in rpc.go.
//

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
