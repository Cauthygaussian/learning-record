package kvraft

import (
	"6.5840/kvsrv1/rpc"
	"6.5840/kvtest1"
	"6.5840/tester1"
)


type Clerk struct {
	clnt    *tester.Clnt
	servers []string
	// You will have to modify this struct.
	LastLeaderID int 
	ClientID int64 
	CommandID int64 
}

func nrand() int64{
	max := big.NewInt(int64(1) << 62)
	bigx, _ := rand.Int(rand.Reader, max)
	x := bigx.Int64()
	return x 
}

func MakeClerk(clnt *tester.Clnt, servers []string) kvtest.IKVClerk {
	ck := &Clerk{clnt: clnt, servers: servers}
	// You'll have to add code here.
	ck.LastLeaderID = 0
	ck.ClientID = nrand()
	ck.CommandID = 0
	return ck
}

// Get fetches the current value and version for a key.  It returns
// ErrNoKey if the key does not exist. It keeps trying forever in the
// face of all other errors.
//
// You can send an RPC to server i with code like this:
// ok := ck.clnt.Call(ck.servers[i], "KVServer.Get", &args, &reply)
//
// The types of args and reply (including whether they are pointers)
// must match the declared types of the RPC handler function's
// arguments. Additionally, reply must be passed as a pointer.
func (ck *Clerk) Get(key string) (string, rpc.Tversion, rpc.Err) {

	// You will have to modify this function.
	args := rpc.GetArgs{
		Key : key,
		ClinetID : ck.ClientID ,
		CommandID : ck.CommandID ,
	}
	serverID := ck.LastLeaderID
	for{
		for i := 0; i < len(ck.servers); i++{
			server := ck.servers[serverID]
			getReply := rpc.GetReply{}
			ok := ck.clnt.Call(server, "KVServer.Get", &args, &getReply)
			if ok{
				switch getReply.Err{
				case rpc.OK:
					ck.LastLeaderID = serverID
					return getReply.Value, getReply.Version, rpc.OK
				case rpc.ErrNoKey:
					ck.LastLeaderID = serverID
					return "", 0, rpc.ErrNoKey
				case rpc.ErrWrongLeader:
					//try next server

				}
			}
			serverID = (serverID + 1) % len(ck.servers)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// Put updates key with value only if the version in the
// request matches the version of the key at the server.  If the
// versions numbers don't match, the server should return
// ErrVersion.  If Put receives an ErrVersion on its first RPC, Put
// should return ErrVersion, since the Put was definitely not
// performed at the server. If the server returns ErrVersion on a
// resend RPC, then Put must return ErrMaybe to the application, since
// its earlier RPC might have been processed by the server successfully
// but the response was lost, and the the Clerk doesn't know if
// the Put was performed or not.
//
// You can send an RPC to server i with code like this:
// ok := ck.clnt.Call(ck.servers[i], "KVServer.Put", &args, &reply)
//
// The types of args and reply (including whether they are pointers)
// must match the declared types of the RPC handler function's
// arguments. Additionally, reply must be passed as a pointer.
func (ck *Clerk) Put(key string, value string, version rpc.Tversion) rpc.Err {
	args := rpc.PutArgs{
		Key : key, 
		Value : value, 
		Version :version,
		ClientID : ck.ClientID ,
		CommandID : ck.CommandID ,
	}
	first := true 
	serverID := ck.LastLeaderID
	for{
		for i := 0; i < len(ck.servers); i++{
			server := ck.servers[serverID]
			reply := rpc.PutReply{}
			ok := ck.clnt.Call(ck.servers[ck.LastLeaderID], "KVServer.Put", &args, &reply)
			if ok{
				switch reply.Err{
				case rpc.OK:
					ck.LastLeaderID = serverID
					return rpc.OK 
				case rpc.ErrNoKey:
					ck.LastLeaderID = serverID
					return rpc.ErrNoKey 
				case rpc.ErrVersion:
					ck.LastLeaderID = serverID
					if first{
						return rpc.ErrVersion
					}else{
						return rpc.ErrMaybe
					}
				}
				case rpc.ErrWrongLeader:
					//try next server
			}
			first = false
			ck.LastLeaderID = (ck.LastLeaderID + 1) % len(ck.servers)
			ck.CommandID++
		}
		time.Sleep(100 * time.Millisecond)
	}
	// You will have to modify this function.
	
	return ""
}
