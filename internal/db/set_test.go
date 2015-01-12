// Copyright (C) 2014 The Syncthing Authors.
//
// This program is free software: you can redistribute it and/or modify it
// under the terms of the GNU General Public License as published by the Free
// Software Foundation, either version 3 of the License, or (at your option)
// any later version.
//
// This program is distributed in the hope that it will be useful, but WITHOUT
// ANY WARRANTY; without even the implied warranty of MERCHANTABILITY or
// FITNESS FOR A PARTICULAR PURPOSE. See the GNU General Public License for
// more details.
//
// You should have received a copy of the GNU General Public License along
// with this program. If not, see <http://www.gnu.org/licenses/>.

package db_test

import (
	"bytes"
	"fmt"
	"reflect"
	"sort"
	"testing"

	"github.com/syncthing/syncthing/internal/db"
	"github.com/syncthing/syncthing/internal/lamport"
	"github.com/syncthing/syncthing/internal/protocol"
	"github.com/syndtr/goleveldb/leveldb"
	"github.com/syndtr/goleveldb/leveldb/storage"
)

var remoteDevice0, remoteDevice1 protocol.DeviceID

func init() {
	remoteDevice0, _ = protocol.DeviceIDFromString("AIR6LPZ-7K4PTTV-UXQSMUU-CPQ5YWH-OEDFIIQ-JUG777G-2YQXXR5-YD6AWQR")
	remoteDevice1, _ = protocol.DeviceIDFromString("I6KAH76-66SLLLB-5PFXSOA-UFJCDZC-YAOMLEK-CP2GB32-BV5RQST-3PSROAU")
}

func genBlocks(n int) []protocol.BlockInfo {
	b := make([]protocol.BlockInfo, n)
	for i := range b {
		h := make([]byte, 32)
		for j := range h {
			h[j] = byte(i + j)
		}
		b[i].Size = uint32(i)
		b[i].Hash = h
	}
	return b
}

func globalList(folder string, s *db.FileSet) []protocol.FileInfo {
	var fs []protocol.FileInfo
	s.WithGlobal(folder, func(fi db.FileIntf) bool {
		f := fi.(protocol.FileInfo)
		fs = append(fs, f)
		return true
	})
	return fs
}

func haveList(folder string, s *db.FileSet, n protocol.DeviceID) []protocol.FileInfo {
	var fs []protocol.FileInfo
	s.WithHave(folder, n, func(fi db.FileIntf) bool {
		f := fi.(protocol.FileInfo)
		fs = append(fs, f)
		return true
	})
	return fs
}

func needList(folder string, s *db.FileSet, n protocol.DeviceID) []protocol.FileInfo {
	var fs []protocol.FileInfo
	s.WithNeed(folder, n, func(fi db.FileIntf) bool {
		f := fi.(protocol.FileInfo)
		fs = append(fs, f)
		return true
	})
	return fs
}

type fileList []protocol.FileInfo

func (l fileList) Len() int {
	return len(l)
}

func (l fileList) Less(a, b int) bool {
	return l[a].Name < l[b].Name
}

func (l fileList) Swap(a, b int) {
	l[a], l[b] = l[b], l[a]
}

func (l fileList) String() string {
	var b bytes.Buffer
	b.WriteString("[]protocol.FileList{\n")
	for _, f := range l {
		fmt.Fprintf(&b, "  %q: #%d, %d bytes, %d blocks, flags=%o\n", f.Name, f.Version, f.Size(), len(f.Blocks), f.Flags)
	}
	b.WriteString("}")
	return b.String()
}

func TestGlobalSet(t *testing.T) {
	lamport.Default = lamport.Clock{}

	ldb, err := leveldb.Open(storage.NewMemStorage(), nil)
	if err != nil {
		t.Fatal(err)
	}

	m := db.NewFileSet(ldb)

	local0 := fileList{
		protocol.FileInfo{Name: "a", Version: 1000, Blocks: genBlocks(1)},
		protocol.FileInfo{Name: "b", Version: 1000, Blocks: genBlocks(2)},
		protocol.FileInfo{Name: "c", Version: 1000, Blocks: genBlocks(3)},
		protocol.FileInfo{Name: "d", Version: 1000, Blocks: genBlocks(4)},
		protocol.FileInfo{Name: "z", Version: 1000, Blocks: genBlocks(8)},
	}
	local1 := fileList{
		protocol.FileInfo{Name: "a", Version: 1000, Blocks: genBlocks(1)},
		protocol.FileInfo{Name: "b", Version: 1000, Blocks: genBlocks(2)},
		protocol.FileInfo{Name: "c", Version: 1000, Blocks: genBlocks(3)},
		protocol.FileInfo{Name: "d", Version: 1000, Blocks: genBlocks(4)},
	}
	localTot := fileList{
		local0[0],
		local0[1],
		local0[2],
		local0[3],
		protocol.FileInfo{Name: "z", Version: 1001, Flags: protocol.FlagDeleted},
	}

	remote0 := fileList{
		protocol.FileInfo{Name: "a", Version: 1000, Blocks: genBlocks(1)},
		protocol.FileInfo{Name: "b", Version: 1000, Blocks: genBlocks(2)},
		protocol.FileInfo{Name: "c", Version: 1002, Blocks: genBlocks(5)},
	}
	remote1 := fileList{
		protocol.FileInfo{Name: "b", Version: 1001, Blocks: genBlocks(6)},
		protocol.FileInfo{Name: "e", Version: 1000, Blocks: genBlocks(7)},
	}
	remoteTot := fileList{
		remote0[0],
		remote1[0],
		remote0[2],
		remote1[1],
	}

	expectedGlobal := fileList{
		remote0[0],  // a
		remote1[0],  // b
		remote0[2],  // c
		localTot[3], // d
		remote1[1],  // e
		localTot[4], // z
	}

	expectedLocalNeed := fileList{
		remote1[0],
		remote0[2],
		remote1[1],
	}

	expectedRemoteNeed := fileList{
		local0[3],
	}

	m.ReplaceWithDelete("folder1", protocol.LocalDeviceID, local0)
	m.ReplaceWithDelete("folder1", protocol.LocalDeviceID, local1)
	m.Replace("folder1", remoteDevice0, remote0)
	m.Update("folder1", remoteDevice0, remote1)

	g := fileList(globalList("folder1", m))
	sort.Sort(g)

	if fmt.Sprint(g) != fmt.Sprint(expectedGlobal) {
		t.Errorf("Global incorrect;\n A: %v !=\n E: %v", g, expectedGlobal)
	}

	h := fileList(haveList("folder1", m, protocol.LocalDeviceID))
	sort.Sort(h)

	if fmt.Sprint(h) != fmt.Sprint(localTot) {
		t.Errorf("Have incorrect;\n A: %v !=\n E: %v", h, localTot)
	}

	h = fileList(haveList("folder1", m, remoteDevice0))
	sort.Sort(h)

	if fmt.Sprint(h) != fmt.Sprint(remoteTot) {
		t.Errorf("Have incorrect;\n A: %v !=\n E: %v", h, remoteTot)
	}

	n := fileList(needList("folder1", m, protocol.LocalDeviceID))
	sort.Sort(n)

	if fmt.Sprint(n) != fmt.Sprint(expectedLocalNeed) {
		t.Errorf("Need incorrect;\n A: %v !=\n E: %v", n, expectedLocalNeed)
	}

	n = fileList(needList("folder1", m, remoteDevice0))
	sort.Sort(n)

	if fmt.Sprint(n) != fmt.Sprint(expectedRemoteNeed) {
		t.Errorf("Need incorrect;\n A: %v !=\n E: %v", n, expectedRemoteNeed)
	}

	f, ok := m.Get("folder1", protocol.LocalDeviceID, "b")
	if !ok {
		t.Error("Unexpectedly not OK")
	}
	if fmt.Sprint(f) != fmt.Sprint(localTot[1]) {
		t.Errorf("Get incorrect;\n A: %v !=\n E: %v", f, localTot[1])
	}

	f, ok = m.Get("folder1", remoteDevice0, "b")
	if !ok {
		t.Error("Unexpectedly not OK")
	}
	if fmt.Sprint(f) != fmt.Sprint(remote1[0]) {
		t.Errorf("Get incorrect;\n A: %v !=\n E: %v", f, remote1[0])
	}

	f, ok = m.GetGlobal("folder1", "b")
	if !ok {
		t.Error("Unexpectedly not OK")
	}
	if fmt.Sprint(f) != fmt.Sprint(remote1[0]) {
		t.Errorf("GetGlobal incorrect;\n A: %v !=\n E: %v", f, remote1[0])
	}

	f, ok = m.Get("folder1", protocol.LocalDeviceID, "zz")
	if ok {
		t.Error("Unexpectedly OK")
	}
	if f.Name != "" {
		t.Errorf("Get incorrect;\n A: %v !=\n E: %v", f, protocol.FileInfo{})
	}

	f, ok = m.GetGlobal("folder1", "zz")
	if ok {
		t.Error("Unexpectedly OK")
	}
	if f.Name != "" {
		t.Errorf("GetGlobal incorrect;\n A: %v !=\n E: %v", f, protocol.FileInfo{})
	}

	av := []protocol.DeviceID{protocol.LocalDeviceID, remoteDevice0}
	a := m.Availability("folder1", "a")
	if !(len(a) == 2 && (a[0] == av[0] && a[1] == av[1] || a[0] == av[1] && a[1] == av[0])) {
		t.Errorf("Availability incorrect;\n A: %v !=\n E: %v", a, av)
	}
	a = m.Availability("folder1", "b")
	if len(a) != 1 || a[0] != remoteDevice0 {
		t.Errorf("Availability incorrect;\n A: %v !=\n E: %v", a, remoteDevice0)
	}
	a = m.Availability("folder1", "d")
	if len(a) != 1 || a[0] != protocol.LocalDeviceID {
		t.Errorf("Availability incorrect;\n A: %v !=\n E: %v", a, protocol.LocalDeviceID)
	}
}

func TestNeedWithInvalid(t *testing.T) {
	lamport.Default = lamport.Clock{}

	ldb, err := leveldb.Open(storage.NewMemStorage(), nil)
	if err != nil {
		t.Fatal(err)
	}

	s := db.NewFileSet(ldb)

	localHave := fileList{
		protocol.FileInfo{Name: "a", Version: 1000, Blocks: genBlocks(1)},
	}
	remote0Have := fileList{
		protocol.FileInfo{Name: "b", Version: 1001, Blocks: genBlocks(2)},
		protocol.FileInfo{Name: "c", Version: 1002, Blocks: genBlocks(5), Flags: protocol.FlagInvalid},
		protocol.FileInfo{Name: "d", Version: 1003, Blocks: genBlocks(7)},
	}
	remote1Have := fileList{
		protocol.FileInfo{Name: "c", Version: 1002, Blocks: genBlocks(7)},
		protocol.FileInfo{Name: "d", Version: 1003, Blocks: genBlocks(5), Flags: protocol.FlagInvalid},
		protocol.FileInfo{Name: "e", Version: 1004, Blocks: genBlocks(5), Flags: protocol.FlagInvalid},
	}

	expectedNeed := fileList{
		protocol.FileInfo{Name: "b", Version: 1001, Blocks: genBlocks(2)},
		protocol.FileInfo{Name: "c", Version: 1002, Blocks: genBlocks(7)},
		protocol.FileInfo{Name: "d", Version: 1003, Blocks: genBlocks(7)},
	}

	s.ReplaceWithDelete("folder1", protocol.LocalDeviceID, localHave)
	s.Replace("folder1", remoteDevice0, remote0Have)
	s.Replace("folder1", remoteDevice1, remote1Have)

	need := fileList(needList("folder1", s, protocol.LocalDeviceID))
	sort.Sort(need)

	if fmt.Sprint(need) != fmt.Sprint(expectedNeed) {
		t.Errorf("Need incorrect;\n A: %v !=\n E: %v", need, expectedNeed)
	}
}

func TestUpdateToInvalid(t *testing.T) {
	lamport.Default = lamport.Clock{}

	ldb, err := leveldb.Open(storage.NewMemStorage(), nil)
	if err != nil {
		t.Fatal(err)
	}

	s := db.NewFileSet(ldb)

	localHave := fileList{
		protocol.FileInfo{Name: "a", Version: 1000, Blocks: genBlocks(1)},
		protocol.FileInfo{Name: "b", Version: 1001, Blocks: genBlocks(2)},
		protocol.FileInfo{Name: "c", Version: 1002, Blocks: genBlocks(5), Flags: protocol.FlagInvalid},
		protocol.FileInfo{Name: "d", Version: 1003, Blocks: genBlocks(7)},
	}

	s.ReplaceWithDelete("folder1", protocol.LocalDeviceID, localHave)

	have := fileList(haveList("folder1", s, protocol.LocalDeviceID))
	sort.Sort(have)

	if fmt.Sprint(have) != fmt.Sprint(localHave) {
		t.Errorf("Have incorrect before invalidation;\n A: %v !=\n E: %v", have, localHave)
	}

	localHave[1] = protocol.FileInfo{Name: "b", Version: 1001, Flags: protocol.FlagInvalid}
	s.Update("folder1", protocol.LocalDeviceID, localHave[1:2])

	have = fileList(haveList("folder1", s, protocol.LocalDeviceID))
	sort.Sort(have)

	if fmt.Sprint(have) != fmt.Sprint(localHave) {
		t.Errorf("Have incorrect after invalidation;\n A: %v !=\n E: %v", have, localHave)
	}
}

func TestInvalidAvailability(t *testing.T) {
	lamport.Default = lamport.Clock{}

	ldb, err := leveldb.Open(storage.NewMemStorage(), nil)
	if err != nil {
		t.Fatal(err)
	}

	s := db.NewFileSet(ldb)

	remote0Have := fileList{
		protocol.FileInfo{Name: "both", Version: 1001, Blocks: genBlocks(2)},
		protocol.FileInfo{Name: "r1only", Version: 1002, Blocks: genBlocks(5), Flags: protocol.FlagInvalid},
		protocol.FileInfo{Name: "r0only", Version: 1003, Blocks: genBlocks(7)},
		protocol.FileInfo{Name: "none", Version: 1004, Blocks: genBlocks(5), Flags: protocol.FlagInvalid},
	}
	remote1Have := fileList{
		protocol.FileInfo{Name: "both", Version: 1001, Blocks: genBlocks(2)},
		protocol.FileInfo{Name: "r1only", Version: 1002, Blocks: genBlocks(7)},
		protocol.FileInfo{Name: "r0only", Version: 1003, Blocks: genBlocks(5), Flags: protocol.FlagInvalid},
		protocol.FileInfo{Name: "none", Version: 1004, Blocks: genBlocks(5), Flags: protocol.FlagInvalid},
	}

	s.Replace("folder1", remoteDevice0, remote0Have)
	s.Replace("folder1", remoteDevice1, remote1Have)

	if av := s.Availability("folder1", "both"); len(av) != 2 {
		t.Error("Incorrect availability for 'both':", av)
	}

	if av := s.Availability("folder1", "r0only"); len(av) != 1 || av[0] != remoteDevice0 {
		t.Error("Incorrect availability for 'r0only':", av)
	}

	if av := s.Availability("folder1", "r1only"); len(av) != 1 || av[0] != remoteDevice1 {
		t.Error("Incorrect availability for 'r1only':", av)
	}

	if av := s.Availability("folder1", "none"); len(av) != 0 {
		t.Error("Incorrect availability for 'none':", av)
	}
}

func TestLocalDeleted(t *testing.T) {
	ldb, err := leveldb.Open(storage.NewMemStorage(), nil)
	if err != nil {
		t.Fatal(err)
	}
	m := db.NewFileSet(ldb)
	lamport.Default = lamport.Clock{}

	local1 := []protocol.FileInfo{
		{Name: "a", Version: 1000},
		{Name: "b", Version: 1000},
		{Name: "c", Version: 1000},
		{Name: "d", Version: 1000},
		{Name: "z", Version: 1000, Flags: protocol.FlagDirectory},
	}

	m.ReplaceWithDelete("folder1", protocol.LocalDeviceID, local1)

	m.ReplaceWithDelete("folder1", protocol.LocalDeviceID, []protocol.FileInfo{
		local1[0],
		// [1] removed
		local1[2],
		local1[3],
		local1[4],
	})
	m.ReplaceWithDelete("folder1", protocol.LocalDeviceID, []protocol.FileInfo{
		local1[0],
		local1[2],
		// [3] removed
		local1[4],
	})
	m.ReplaceWithDelete("folder1", protocol.LocalDeviceID, []protocol.FileInfo{
		local1[0],
		local1[2],
		// [4] removed
	})

	expectedGlobal1 := []protocol.FileInfo{
		local1[0],
		{Name: "b", Version: 1001, Flags: protocol.FlagDeleted},
		local1[2],
		{Name: "d", Version: 1002, Flags: protocol.FlagDeleted},
		{Name: "z", Version: 1003, Flags: protocol.FlagDeleted | protocol.FlagDirectory},
	}

	g := globalList("folder1", m)
	sort.Sort(fileList(g))
	sort.Sort(fileList(expectedGlobal1))

	if fmt.Sprint(g) != fmt.Sprint(expectedGlobal1) {
		t.Errorf("Global incorrect;\n A: %v !=\n E: %v", g, expectedGlobal1)
	}

	m.ReplaceWithDelete("folder1", protocol.LocalDeviceID, []protocol.FileInfo{
		local1[0],
		// [2] removed
	})

	expectedGlobal2 := []protocol.FileInfo{
		local1[0],
		{Name: "b", Version: 1001, Flags: protocol.FlagDeleted},
		{Name: "c", Version: 1004, Flags: protocol.FlagDeleted},
		{Name: "d", Version: 1002, Flags: protocol.FlagDeleted},
		{Name: "z", Version: 1003, Flags: protocol.FlagDeleted | protocol.FlagDirectory},
	}

	g = globalList("folder1", m)
	sort.Sort(fileList(g))
	sort.Sort(fileList(expectedGlobal2))

	if fmt.Sprint(g) != fmt.Sprint(expectedGlobal2) {
		t.Errorf("Global incorrect;\n A: %v !=\n E: %v", g, expectedGlobal2)
	}
}

func Benchmark10kReplace(b *testing.B) {
	ldb, err := leveldb.Open(storage.NewMemStorage(), nil)
	if err != nil {
		b.Fatal(err)
	}

	var local []protocol.FileInfo
	for i := 0; i < 10000; i++ {
		local = append(local, protocol.FileInfo{Name: fmt.Sprintf("file%d", i), Version: 1000})
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m := db.NewFileSet(ldb)
		m.ReplaceWithDelete("folder1", protocol.LocalDeviceID, local)
	}
}

func Benchmark10kUpdateChg(b *testing.B) {
	var remote []protocol.FileInfo
	for i := 0; i < 10000; i++ {
		remote = append(remote, protocol.FileInfo{Name: fmt.Sprintf("file%d", i), Version: 1000})
	}

	ldb, err := leveldb.Open(storage.NewMemStorage(), nil)
	if err != nil {
		b.Fatal(err)
	}

	m := db.NewFileSet(ldb)
	m.Replace("folder1", remoteDevice0, remote)

	var local []protocol.FileInfo
	for i := 0; i < 10000; i++ {
		local = append(local, protocol.FileInfo{Name: fmt.Sprintf("file%d", i), Version: 1000})
	}

	m.ReplaceWithDelete("folder1", protocol.LocalDeviceID, local)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		for j := range local {
			local[j].Version++
		}
		b.StartTimer()
		m.Update("folder1", protocol.LocalDeviceID, local)
	}
}

func Benchmark10kUpdateSme(b *testing.B) {
	var remote []protocol.FileInfo
	for i := 0; i < 10000; i++ {
		remote = append(remote, protocol.FileInfo{Name: fmt.Sprintf("file%d", i), Version: 1000})
	}

	ldb, err := leveldb.Open(storage.NewMemStorage(), nil)
	if err != nil {
		b.Fatal(err)
	}
	m := db.NewFileSet(ldb)
	m.Replace("folder1", remoteDevice0, remote)

	var local []protocol.FileInfo
	for i := 0; i < 10000; i++ {
		local = append(local, protocol.FileInfo{Name: fmt.Sprintf("file%d", i), Version: 1000})
	}

	m.ReplaceWithDelete("folder1", protocol.LocalDeviceID, local)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.Update("folder1", protocol.LocalDeviceID, local)
	}
}

func Benchmark10kNeed2k(b *testing.B) {
	var remote []protocol.FileInfo
	for i := 0; i < 10000; i++ {
		remote = append(remote, protocol.FileInfo{Name: fmt.Sprintf("file%d", i), Version: 1000})
	}

	ldb, err := leveldb.Open(storage.NewMemStorage(), nil)
	if err != nil {
		b.Fatal(err)
	}

	m := db.NewFileSet(ldb)
	m.Replace("folder1", remoteDevice0, remote)

	var local []protocol.FileInfo
	for i := 0; i < 8000; i++ {
		local = append(local, protocol.FileInfo{Name: fmt.Sprintf("file%d", i), Version: 1000})
	}
	for i := 8000; i < 10000; i++ {
		local = append(local, protocol.FileInfo{Name: fmt.Sprintf("file%d", i), Version: 980})
	}

	m.ReplaceWithDelete("folder1", protocol.LocalDeviceID, local)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		fs := needList("folder1", m, protocol.LocalDeviceID)
		if l := len(fs); l != 2000 {
			b.Errorf("wrong length %d != 2k", l)
		}
	}
}

func Benchmark10kHaveFullList(b *testing.B) {
	var remote []protocol.FileInfo
	for i := 0; i < 10000; i++ {
		remote = append(remote, protocol.FileInfo{Name: fmt.Sprintf("file%d", i), Version: 1000})
	}

	ldb, err := leveldb.Open(storage.NewMemStorage(), nil)
	if err != nil {
		b.Fatal(err)
	}

	m := db.NewFileSet(ldb)
	m.Replace("folder1", remoteDevice0, remote)

	var local []protocol.FileInfo
	for i := 0; i < 2000; i++ {
		local = append(local, protocol.FileInfo{Name: fmt.Sprintf("file%d", i), Version: 1000})
	}
	for i := 2000; i < 10000; i++ {
		local = append(local, protocol.FileInfo{Name: fmt.Sprintf("file%d", i), Version: 980})
	}

	m.ReplaceWithDelete("folder1", protocol.LocalDeviceID, local)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		fs := haveList("folder1", m, protocol.LocalDeviceID)
		if l := len(fs); l != 10000 {
			b.Errorf("wrong length %d != 10k", l)
		}
	}
}

func Benchmark10kGlobal(b *testing.B) {
	var remote []protocol.FileInfo
	for i := 0; i < 10000; i++ {
		remote = append(remote, protocol.FileInfo{Name: fmt.Sprintf("file%d", i), Version: 1000})
	}

	ldb, err := leveldb.Open(storage.NewMemStorage(), nil)
	if err != nil {
		b.Fatal(err)
	}

	m := db.NewFileSet(ldb)
	m.Replace("folder1", remoteDevice0, remote)

	var local []protocol.FileInfo
	for i := 0; i < 2000; i++ {
		local = append(local, protocol.FileInfo{Name: fmt.Sprintf("file%d", i), Version: 1000})
	}
	for i := 2000; i < 10000; i++ {
		local = append(local, protocol.FileInfo{Name: fmt.Sprintf("file%d", i), Version: 980})
	}

	m.ReplaceWithDelete("folder1", protocol.LocalDeviceID, local)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		fs := globalList("folder1", m)
		if l := len(fs); l != 10000 {
			b.Errorf("wrong length %d != 10k", l)
		}
	}
}

func TestGlobalReset(t *testing.T) {
	ldb, err := leveldb.Open(storage.NewMemStorage(), nil)
	if err != nil {
		t.Fatal(err)
	}

	m := db.NewFileSet(ldb)

	local := []protocol.FileInfo{
		{Name: "a", Version: 1000},
		{Name: "b", Version: 1000},
		{Name: "c", Version: 1000},
		{Name: "d", Version: 1000},
	}

	remote := []protocol.FileInfo{
		{Name: "a", Version: 1000},
		{Name: "b", Version: 1001},
		{Name: "c", Version: 1002},
		{Name: "e", Version: 1000},
	}

	m.ReplaceWithDelete("folder1", protocol.LocalDeviceID, local)
	g := globalList("folder1", m)
	sort.Sort(fileList(g))

	if fmt.Sprint(g) != fmt.Sprint(local) {
		t.Errorf("Global incorrect;\n%v !=\n%v", g, local)
	}

	m.Replace("folder1", remoteDevice0, remote)
	m.Replace("folder1", remoteDevice0, nil)

	g = globalList("folder1", m)
	sort.Sort(fileList(g))

	if fmt.Sprint(g) != fmt.Sprint(local) {
		t.Errorf("Global incorrect;\n%v !=\n%v", g, local)
	}
}

func TestNeed(t *testing.T) {
	ldb, err := leveldb.Open(storage.NewMemStorage(), nil)
	if err != nil {
		t.Fatal(err)
	}

	m := db.NewFileSet(ldb)

	local := []protocol.FileInfo{
		{Name: "a", Version: 1000},
		{Name: "b", Version: 1000},
		{Name: "c", Version: 1000},
		{Name: "d", Version: 1000},
	}

	remote := []protocol.FileInfo{
		{Name: "a", Version: 1000},
		{Name: "b", Version: 1001},
		{Name: "c", Version: 1002},
		{Name: "e", Version: 1000},
	}

	shouldNeed := []protocol.FileInfo{
		{Name: "b", Version: 1001},
		{Name: "c", Version: 1002},
		{Name: "e", Version: 1000},
	}

	m.ReplaceWithDelete("folder1", protocol.LocalDeviceID, local)
	m.Replace("folder1", remoteDevice0, remote)

	need := needList("folder1", m, protocol.LocalDeviceID)

	sort.Sort(fileList(need))
	sort.Sort(fileList(shouldNeed))

	if fmt.Sprint(need) != fmt.Sprint(shouldNeed) {
		t.Errorf("Need incorrect;\n%v !=\n%v", need, shouldNeed)
	}
}

func TestLocalVersion(t *testing.T) {
	ldb, err := leveldb.Open(storage.NewMemStorage(), nil)
	if err != nil {
		t.Fatal(err)
	}

	m := db.NewFileSet(ldb)

	local1 := []protocol.FileInfo{
		{Name: "a", Version: 1000},
		{Name: "b", Version: 1000},
		{Name: "c", Version: 1000},
		{Name: "d", Version: 1000},
	}

	local2 := []protocol.FileInfo{
		local1[0],
		// [1] deleted
		local1[2],
		{Name: "d", Version: 1002},
		{Name: "e", Version: 1000},
	}

	m.ReplaceWithDelete("folder1", protocol.LocalDeviceID, local1)
	c0 := m.LocalVersion("folder1", protocol.LocalDeviceID)

	m.ReplaceWithDelete("folder1", protocol.LocalDeviceID, local2)
	c1 := m.LocalVersion("folder1", protocol.LocalDeviceID)
	if !(c1 > c0) {
		t.Fatal("Local version number should have incremented")
	}

	m.ReplaceWithDelete("folder1", protocol.LocalDeviceID, local2)
	c2 := m.LocalVersion("folder1", protocol.LocalDeviceID)
	if c2 != c1 {
		t.Fatal("Local version number should be unchanged")
	}
}

func TestListDropFolder(t *testing.T) {
	ldb, err := leveldb.Open(storage.NewMemStorage(), nil)
	if err != nil {
		t.Fatal(err)
	}

	s0 := db.NewFileSet(ldb)
	local1 := []protocol.FileInfo{
		{Name: "a", Version: 1000},
		{Name: "b", Version: 1000},
		{Name: "c", Version: 1000},
	}
	s0.Replace("test0", protocol.LocalDeviceID, local1)

	s1 := db.NewFileSet(ldb)
	local2 := []protocol.FileInfo{
		{Name: "d", Version: 1002},
		{Name: "e", Version: 1002},
		{Name: "f", Version: 1002},
	}
	s1.Replace("test1", remoteDevice0, local2)

	// Check that we have both folders and their data is in the global list

	expectedFolderList := []string{"test0", "test1"}
	if actualFolderList := db.ListFolders(ldb); !reflect.DeepEqual(actualFolderList, expectedFolderList) {
		t.Fatalf("FolderList mismatch\nE: %v\nA: %v", expectedFolderList, actualFolderList)
	}
	if l := len(globalList("test0", s0)); l != 3 {
		t.Errorf("Incorrect global length %d != 3 for s0", l)
	}
	if l := len(globalList("test1", s1)); l != 3 {
		t.Errorf("Incorrect global length %d != 3 for s1", l)
	}

	// Drop one of them and check that it's gone.

	db.DropFolder(ldb, "test1")

	expectedFolderList = []string{"test0"}
	if actualFolderList := db.ListFolders(ldb); !reflect.DeepEqual(actualFolderList, expectedFolderList) {
		t.Fatalf("FolderList mismatch\nE: %v\nA: %v", expectedFolderList, actualFolderList)
	}
	if l := len(globalList("test0", s0)); l != 3 {
		t.Errorf("Incorrect global length %d != 3 for s0", l)
	}
	if l := len(globalList("test1", s1)); l != 0 {
		t.Errorf("Incorrect global length %d != 0 for s1", l)
	}
}

func TestGlobalNeedWithInvalid(t *testing.T) {
	ldb, err := leveldb.Open(storage.NewMemStorage(), nil)
	if err != nil {
		t.Fatal(err)
	}

	s := db.NewFileSet(ldb)

	rem0 := fileList{
		protocol.FileInfo{Name: "a", Version: 1002, Blocks: genBlocks(4)},
		protocol.FileInfo{Name: "b", Version: 1002, Flags: protocol.FlagInvalid},
		protocol.FileInfo{Name: "c", Version: 1002, Blocks: genBlocks(4)},
	}
	s.Replace("folder1", remoteDevice0, rem0)

	rem1 := fileList{
		protocol.FileInfo{Name: "a", Version: 1002, Blocks: genBlocks(4)},
		protocol.FileInfo{Name: "b", Version: 1002, Blocks: genBlocks(4)},
		protocol.FileInfo{Name: "c", Version: 1002, Flags: protocol.FlagInvalid},
	}
	s.Replace("folder1", remoteDevice1, rem1)

	total := fileList{
		// There's a valid copy of each file, so it should be merged
		protocol.FileInfo{Name: "a", Version: 1002, Blocks: genBlocks(4)},
		protocol.FileInfo{Name: "b", Version: 1002, Blocks: genBlocks(4)},
		protocol.FileInfo{Name: "c", Version: 1002, Blocks: genBlocks(4)},
	}

	need := fileList(needList("folder1", s, protocol.LocalDeviceID))
	if fmt.Sprint(need) != fmt.Sprint(total) {
		t.Errorf("Need incorrect;\n A: %v !=\n E: %v", need, total)
	}

	global := fileList(globalList("folder1", s))
	if fmt.Sprint(global) != fmt.Sprint(total) {
		t.Errorf("Global incorrect;\n A: %v !=\n E: %v", global, total)
	}
}

func TestLongPath(t *testing.T) {
	ldb, err := leveldb.Open(storage.NewMemStorage(), nil)
	if err != nil {
		t.Fatal(err)
	}

	s := db.NewFileSet(ldb)

	var b bytes.Buffer
	for i := 0; i < 100; i++ {
		b.WriteString("012345678901234567890123456789012345678901234567890")
	}
	name := b.String() // 5000 characters

	local := []protocol.FileInfo{
		{Name: string(name), Version: 1000},
	}

	s.ReplaceWithDelete("folder1", protocol.LocalDeviceID, local)

	gf := globalList("folder1", s)
	if l := len(gf); l != 1 {
		t.Fatalf("Incorrect len %d != 1 for global list", l)
	}
	if gf[0].Name != local[0].Name {
		t.Errorf("Incorrect long filename;\n%q !=\n%q",
			gf[0].Name, local[0].Name)
	}
}
