package mr

import "log"
import "time"
import "sync"
import "net"
import "os"
import "net/rpc"
import "net/http"


const(
	Unassigned = iota //任务未分配
	Assigned         //任务已分配但未完成
	Completed 	  //任务已完成
	Failed        //任务失败
)

type TaskInfo struct{
	TaskStatus int 
	TaskFile string 
	TaskTime time.Time
}

type Coordinator struct {
	// Your definitions here.
	ReduceTasks []TaskInfo 
	MapTasks []TaskInfo
	NReduce int
	NMap  int
	AllMapTasksDone bool 
	AllReduceTasksDone bool 
	Mutex sync.Mutex
}

// Your code here -- RPC handlers for the worker to call.

//
// an example RPC handler.
//
// the RPC argument and reply types are defined in rpc.go.

func (c *Coordinator) InitTask(file []string) {
	// initialize task slices before indexing into them
    c.MapTasks = make([]TaskInfo, len(file))
    c.ReduceTasks = make([]TaskInfo, c.NReduce)

	
	for idx := 0; idx < len(file); idx++{
		c.MapTasks[idx].TaskStatus = Unassigned 
		c.MapTasks[idx].TaskFile = file[idx]
		c.MapTasks[idx].TaskTime = time.Now()
	}

	for idx := 0; idx < c.NReduce; idx++ {
		c.ReduceTasks[idx].TaskStatus = Unassigned
		c.ReduceTasks[idx].TaskTime = time.Now()
	}
}


func (c *Coordinator) RequestTask(args *MessageSend, reply *MessageReply) error{
	c.Mutex.Lock()
	defer c.Mutex.Unlock()

	NMapTaskCompleted := 0
	NReduceTaskCompleted := 0

	if c.AllMapTasksDone == false{
		//分配Map任务
		for index := 0; index < c.NMap; index++{
			if c.MapTasks[index].TaskStatus == Unassigned || c.MapTasks[index].TaskStatus == Failed || c.MapTasks[index].TaskStatus == Assigned && time.Since(c.MapTasks[index].TaskTime) > 10 * time.Second{
				reply.TaskType = MapTask 
				reply.TaskID = index 
				reply.TaskFile = c.MapTasks[index].TaskFile 
				reply.NMap = c.NMap
				reply.NReduce = c.NReduce
				c.MapTasks[index].TaskStatus = Assigned 
				c.MapTasks[index].TaskTime = time.Now()
				return nil
			}else if c.MapTasks[index].TaskStatus == Completed{
				NMapTaskCompleted ++
			}
		}

		if NMapTaskCompleted == c.NMap{
			c.AllMapTasksDone = true
		}else{
			reply.TaskType = Wait
			return nil 
		}
	}

	if c.AllReduceTasksDone == false{
		//分配Reduce任务
		for index := 0; index < c.NReduce; index++{
			if c.ReduceTasks[index].TaskStatus == Unassigned || c.ReduceTasks[index].TaskStatus == Failed || c.ReduceTasks[index].TaskStatus == Assigned && time.Since(c.ReduceTasks[index].TaskTime) > 10 * time.Second{
				reply.TaskType = ReduceTask
				reply.TaskID = index
				reply.NMap = c.NMap
				reply.NReduce = c.NReduce
				c.ReduceTasks[index].TaskStatus = Assigned
				c.ReduceTasks[index].TaskTime = time.Now()
				return nil
			}else if c.ReduceTasks[index].TaskStatus == Completed{
				NReduceTaskCompleted ++
			}
		}

		if NReduceTaskCompleted == c.NReduce{
			c.AllReduceTasksDone = true 
		}else{
			reply.TaskType = Wait
			return nil 
		}
	}

	reply.TaskType = Exit
	return nil 
}

func (c *Coordinator) ReportTask(args *MessageSend, reply *MessageReply) error{
	c.Mutex.Lock()
	defer c.Mutex.Unlock()

	if args.TaskStatus == MapTaskCompleted{
		c.MapTasks[args.TaskID].TaskStatus = Completed 
	}else if args.TaskStatus == ReduceTaskCompleted{
		c.ReduceTasks[args.TaskID].TaskStatus = Completed 
	}else if args.TaskStatus == MapTaskFailed{
		c.MapTasks[args.TaskID].TaskStatus = Failed 
	}else if args.TaskStatus == ReduceTaskFailed{
		c.ReduceTasks[args.TaskID].TaskStatus = Failed
	}	
	return nil 
}

//
// start a thread that listens for RPCs from worker.go
//
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

//
// main/mrcoordinator.go calls Done() periodically to find out
// if the entire job has finished.
//
func (c *Coordinator) Done() bool {
	ret := false

	// Your code here.
	if c.AllMapTasksDone && c.AllReduceTasksDone{
		ret = true 
	}

	return ret
}

//
// create a Coordinator.
// main/mrcoordinator.go calls this function.
// nReduce is the number of reduce tasks to use.
//
func MakeCoordinator(files []string, nReduce int) *Coordinator {
	c := Coordinator{
		NReduce: nReduce, 
		NMap: len(files), 
		AllMapTasksDone: false, 
		AllReduceTasksDone: false,
	}

	// Your code here.
	c.InitTask(files)

	c.server()
	return &c
}
