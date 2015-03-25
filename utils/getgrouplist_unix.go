// +build linux
// +build cgo

package utils

/*
#define _BSD_SOURCE
#include <grp.h>
#include <stdlib.h>

static int my_getgrouplist(char *user, int gid, gid_t *groups, int *ngroups) {
	return getgrouplist(user, gid, groups, ngroups);
}
*/
import "C"

import (
	"os/user"
	"reflect"
	"strconv"
	"unsafe"

	"sshproxy/group.go"
)

// GetGroupList returns a map of group memberships for the specified user.
//
// It can be used to quickly check if a user is in a specified group.
func GetGroupList(username string) (map[string]bool, error) {
	var (
		groups  *C.gid_t
		ngroups C.int
	)

	u, err := user.Lookup(username)
	if err != nil {
		return nil, err
	}

	gid, err := strconv.Atoi(u.Gid)
	if err != nil {
		return nil, err
	}

	cusername := C.CString(username)
	defer C.free(unsafe.Pointer(cusername))

	ngroups = 128
	groups = (*C.gid_t)(C.malloc(C.size_t(ngroups * 4)))
	defer func() { C.free(unsafe.Pointer(groups)) }() // cannot call C.free(buf) directly if we reallocate the buffer

	for success := false; !success; {
		rv := C.my_getgrouplist(cusername, C.int(gid), groups, &ngroups)
		if rv == -1 {
			// ngroups should be the number of groups according to getgrouplist(3)
			groups = (*C.gid_t)(C.realloc(unsafe.Pointer(groups), C.size_t(ngroups*4)))
		} else {
			success = true
		}
	}

	// Create a slice over the C groups gid_t* array
	var raw_gr_mem []C.gid_t
	sliceHeader := (*reflect.SliceHeader)((unsafe.Pointer(&raw_gr_mem)))
	sliceHeader.Cap = int(ngroups)
	sliceHeader.Len = int(ngroups)
	sliceHeader.Data = uintptr(unsafe.Pointer(groups))

	grps := make(map[string]bool)
	for _, gid := range raw_gr_mem {
		g, err := group.LookupId(int(gid))
		if err != nil {
			return nil, err
		}
		grps[g.Name] = true
	}

	return grps, nil
}
