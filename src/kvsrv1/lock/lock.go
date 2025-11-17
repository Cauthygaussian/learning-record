package lock

import (
	"6.5840/kvtest1"
	"6.5840/kvsrv1/rpc"
	"time"
	"fmt"
	"math/rand"
)

type Lock struct {
	// IKVClerk is a go interface for k/v clerks: the interface hides
	// the specific Clerk type of ck but promises that ck supports
	// Put and Get.  The tester passes the clerk in when calling
	// MakeLock().
	ck kvtest.IKVClerk
	// You may add code here
	Key string 
	id string 
}

// The tester calls MakeLock() and passes in a k/v clerk; your code can
// perform a Put or Get by calling lk.ck.Put() or lk.ck.Get().
//
// Use l as the key to store the "lock state" (you would have to decide
// precisely what the lock state is).
func MakeLock(ck kvtest.IKVClerk, l string) *Lock {
	rand.Seed(time.Now().UnixNano())
	unique := fmt.Sprintf("%d-%d", rand.Int(),time.Now().UnixNano())

	lk := &Lock{ck: ck, Key: l, id: unique}
	// You may add code here
	return lk
}

func (lk *Lock) Acquire() {
	// Your code here
	for{
		id1, ver1, err := lk.ck.Get(lk.Key)
		if err == rpc.OK{
			if id1 == lk.id{
				return 
			}else if id1 == ""{
				// The key does not exist, so we can create it
				perr := lk.ck.Put(lk.Key, lk.id, ver1)
				if perr == rpc.OK{
					return 
				}else if perr == rpc.ErrMaybe{
					// The put may have succeeded, so we try again
					id2, _, _ := lk.ck.Get(lk.Key)
					if id2 == lk.id{
						return 
					}
				}
			}
		}else if err == rpc.ErrNoKey{
			perr := lk.ck.Put(lk.Key, lk.id, ver1)
			if perr == rpc.OK{
				return 
			}else if perr == rpc.ErrMaybe{
				id2, _, _ := lk.ck.Get(lk.Key)
				if id2 == lk.id{
					return 
				}
			}
		}
		time.Sleep(1 * time.Millisecond) // wait before retrying
	}
}

func (lk *Lock) Release() {
	// Your code here
	id1, ver1, err := lk.ck.Get(lk.Key)
	if err == rpc.OK {
		if id1 != lk.id{
			return 
		}else {
			// We can release the lock by deleting the key
			perr := lk.ck.Put(lk.Key, "", ver1)
			if perr == rpc.OK{
				return 
			}
		}
	}else if err == rpc.ErrNoKey{
		return 
	}
}
