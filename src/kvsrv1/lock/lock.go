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
	unique := fmt.Sprintf("%d", rand.Int())

	lk := &Lock{ck: ck, Key: l, id: unique}
	// You may add code here
	return lk
}

func (lk *Lock) Acquire() {
	// Your code here
	for {
		v, ver, err := lk.ck.Get(lk.Key)
		if err == rpc.OK {
			if v == lk.id { // already hold the lock
				return
			}
			if v == "" { // free, try to acquire
				perr := lk.ck.Put(lk.Key, lk.id, ver)
				if perr == rpc.OK {
					return
				}
				if perr == rpc.ErrMaybe { // confirm
					v2, _, _ := lk.ck.Get(lk.Key)
					if v2 == lk.id {
						return
					}
				}
				// ErrVersion -> someone else won; retry
			}
			// held by others -> wait and retry
		} else if err == rpc.ErrNoKey { // treat as empty key with version 0
			perr := lk.ck.Put(lk.Key, lk.id, ver)
			if perr == rpc.OK {
				return
			}
			if perr == rpc.ErrMaybe {
				v2, _, _ := lk.ck.Get(lk.Key)
				if v2 == lk.id {
					return
				}
			}
		}
		time.Sleep(1 * time.Millisecond)
	}
}

func (lk *Lock) Release() {
	// Your code here
	for {
		v, ver, err := lk.ck.Get(lk.Key)
		if err == rpc.OK {
			if v != lk.id { // not owner; consider released
				return
			}
			perr := lk.ck.Put(lk.Key, "", ver)
			if perr == rpc.OK || perr == rpc.ErrMaybe {
				return
			}
			// ErrVersion -> changed by others; loop to observe state
		} else if err == rpc.ErrNoKey {
			// treat as already released
			return
		}
		time.Sleep(1 * time.Millisecond)
	}
	
}
