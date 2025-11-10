package mr

import "log"
import "time"
import "sync"
import "net"
import "os"
import "net/rpc"
import "net/http"

const (
	Unassigned = iota // 任务未分配
	Assigned          // 任务已分配但未完成
	Completed         // 任务已完成
	Failed            // 任务失败
)

type TaskInfo struct {
	TaskStatus int
	TaskFile   string
	TimeStamp  time.Time
}

type Coordinator struct {
	// Your definitions here.
	NMap                   int
	NReduce                int
	MapTasks               []TaskInfo
	ReduceTasks            []TaskInfo
	AllMapTaskCompleted    bool
	AllReduceTaskCompleted bool
	Mutex                  sync.Mutex
}

// Your code here -- RPC handlers for the worker to call.

// an example RPC handler.
//
// the RPC argument and reply types are defined in rpc.go.
func (c *Coordinator) InitTask(file []string) {
	for idx := range file {
		c.MapTasks[idx] = TaskInfo{
			TaskStatus: Unassigned,
			TaskFile:   file[idx],
			TimeStamp:  time.Now(),
		}
	}

	for idx := range c.ReduceTasks {
		c.ReduceTasks[idx] = TaskInfo{
			TaskStatus: Unassigned,
		}
	}

}

func (c *Coordinator) RequestTask(args *MessageSend, reply *MessageReply) error {
	c.Mutex.Lock()
	defer c.Mutex.Unlock()

	if !c.AllMapTaskCompleted {
		NMapTaskCompleted := 0
		for idx, taskInfo := range c.MapTasks {
			if taskInfo.TaskStatus == Unassigned || taskInfo.TaskStatus == Failed || (taskInfo.TaskStatus == Assigned && time.Since(taskInfo.TimeStamp) > 10*time.Second) {
				reply.TaskFile = taskInfo.TaskFile
				reply.TaskID = idx
				reply.TaskType = MapTask
				reply.NReduce = c.NReduce
				reply.NMap = c.NMap
				c.MapTasks[idx].TaskStatus = Assigned
				c.MapTasks[idx].TimeStamp = time.Now()
				return nil
			} else if taskInfo.TaskStatus == Completed {
				NMapTaskCompleted++
			}
		}

		if NMapTaskCompleted == len(c.MapTasks) {
			c.AllMapTaskCompleted = true
		} else {
			reply.TaskType = Wait
			return nil
		}
	}

	if !c.AllReduceTaskCompleted {
		NREduceTaskCompleted := 0
		for idx, taskInfo := range c.ReduceTasks {
			if taskInfo.TaskStatus == Unassigned || taskInfo.TaskStatus == Failed || (taskInfo.TaskStatus == Assigned && time.Since(taskInfo.TimeStamp) > 10*time.Second) {
				reply.TaskType = ReduceTask
				reply.TaskID = idx
				reply.NReduce = c.NReduce
				reply.NMap = c.NMap
				c.ReduceTasks[idx].TaskStatus = Assigned
				c.ReduceTasks[idx].TimeStamp = time.Now()
				return nil
			} else if taskInfo.TaskStatus == Completed {
				NREduceTaskCompleted++
			}
		}

		if NREduceTaskCompleted == len(c.ReduceTasks) {
			c.AllReduceTaskCompleted = true
		} else {
			reply.TaskType = Wait
			return nil
		}
	}

	reply.TaskType = Exit
	return nil
}

func (c *Coordinator) ReportTask(args *MessageSend, reply *MessageReply) error {
	c.Mutex.Lock()
	defer c.Mutex.Unlock()

	if args.TaskCompletedStatus == MapTaskCompleted {
		c.MapTasks[args.TaskID].TaskStatus = Completed
		return nil
	} else if args.TaskCompletedStatus == MapTaskFailed {
		c.MapTasks[args.TaskID].TaskStatus = Failed
		return nil
	} else if args.TaskCompletedStatus == ReduceTaskCompleted {
		c.ReduceTasks[args.TaskID].TaskStatus = Completed
		return nil
	} else if args.TaskCompletedStatus == ReduceTaskFailed {
		c.ReduceTasks[args.TaskID].TaskStatus = Failed
		return nil
	}

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
func (c *Coordinator) Done() bool {

	for _, taskinfo := range c.MapTasks {
		if taskinfo.TaskStatus != Completed {
			return false
		}
	}

	for _, taskinfo := range c.ReduceTasks {
		if taskinfo.TaskStatus != Completed {
			return false
		}
	}
	return true
}

// create a Coordinator.
// main/mrcoordinator.go calls this function.
// nReduce is the number of reduce tasks to use.
func MakeCoordinator(files []string, nReduce int) *Coordinator {
	c := Coordinator{
		NReduce:                nReduce,
		NMap:                   len(files),
		MapTasks:               make([]TaskInfo, len(files)),
		ReduceTasks:            make([]TaskInfo, nReduce),
		AllMapTaskCompleted:    false,
		AllReduceTaskCompleted: false,
		Mutex:                  sync.Mutex{},
	}
	c.InitTask(files)
	// Your code here.

	c.server()
	return &c
}
