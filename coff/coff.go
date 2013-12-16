package coff

import (
	"debug/pe"
	"encoding/binary"
	"reflect"
	"regexp"
	"sort"
	"strconv"

	"github.com/akavel/rsrc/binutil"
)

type Dir struct { // struct IMAGE_RESOURCE_DIRECTORY
	Characteristics      uint32
	TimeDateStamp        uint32
	MajorVersion         uint16
	MinorVersion         uint16
	NumberOfNamedEntries uint16
	NumberOfIdEntries    uint16
	DirEntries
	Dirs
}

type DirEntries []DirEntry
type Dirs []Dir

type DirEntry struct { // struct IMAGE_RESOURCE_DIRECTORY_ENTRY
	NameOrId     uint32
	OffsetToData uint32
}

type DataEntry struct { // struct IMAGE_RESOURCE_DATA_ENTRY
	OffsetToData uint32
	Size1        uint32
	CodePage     uint32 //FIXME: what value here? for now just using 0
	Reserved     uint32
}

type RelocationEntry struct {
	RVA         uint32 // "offset within the Section's raw data where the address starts."
	SymbolIndex uint32 // "(zero based) index in the Symbol table to which the reference refers."
	Type        uint16
}

type Symbol struct {
	Name           [8]byte
	Value          uint32
	SectionNumber  uint16
	Type           uint16
	StorageClass   uint8
	AuxiliaryCount uint8
}

type StringsHeader struct {
	Length uint32
}

const (
	MASK_SUBDIRECTORY = 1 << 31

	RT_ICON       = 3
	RT_GROUP_ICON = 3 + 11
	RT_MANIFEST   = 24
)

var (
	STRING_RSRC = [8]byte{'.', 'r', 's', 'r', 'c', 0, 0, 0}

	LANG_ENTRY  = DirEntry{NameOrId: 0x0409} //FIXME: language; what value should be here?
	RELOC_ENTRY = RelocationEntry{
		SymbolIndex: 0, // "(zero based) index in the Symbol table to which the reference refers. Once you have loaded the COFF file into memory and know where each symbol is, you find the new updated address for the given symbol and update the reference accordingly."
		Type:        7, // according to ldpe.c, this decodes to: IMAGE_REL_I386_DIR32NB
	}
)

type Coff struct {
	pe.FileHeader
	pe.SectionHeader32

	Dir
	DataEntries []DataEntry
	Data        []interface{}

	Relocations []RelocationEntry
	Symbols     []Symbol
	StringsHeader
}

func NewRSRC() *Coff {
	return &Coff{
		pe.FileHeader{
			Machine:              0x014c, //FIXME: find out how to differentiate this value, or maybe not necessary for Go
			NumberOfSections:     1,      // .rsrc
			TimeDateStamp:        0,      // was also 0 in sample data from MinGW's windres.exe
			NumberOfSymbols:      1,
			SizeOfOptionalHeader: 0,
			Characteristics:      0x0104, //FIXME: copied from windres.exe output, find out what should be here and why
		},
		pe.SectionHeader32{
			Name:            STRING_RSRC,
			Characteristics: 0x40000040, // "INITIALIZED_DATA MEM_READ" ?
		},

		// "directory hierarchy" of .rsrc section: top level goes resource type, then id/name, then language
		Dir{},

		[]DataEntry{},
		[]interface{}{},

		[]RelocationEntry{},

		[]Symbol{Symbol{
			Name:           STRING_RSRC,
			Value:          0,
			SectionNumber:  1,
			Type:           0, // FIXME: wtf?
			StorageClass:   3, // FIXME: is it ok? and uint8? and what does the value mean?
			AuxiliaryCount: 0, // FIXME: wtf?
		}},

		StringsHeader{
			Length: uint32(binary.Size(StringsHeader{})), // empty strings table -- but we must still show size of the table's header...
		},
	}
}

//NOTE: function assumes that 'id' is increasing on each entry
func (coff *Coff) AddResource(kind uint32, id uint16, data interface{}, size uint32) {
	//FIXME: find correct place to insert on all levels, then find index in Data
	coff.Relocations = append(coff.Relocations, RELOC_ENTRY)
	coff.SectionHeader32.NumberOfRelocations++

	// find top level entry, inserting new if necessary at correct sorted position
	entries0 := coff.Dir.DirEntries
	dirs0 := coff.Dir.Dirs
	i0 := sort.Search(len(entries0), func(i int) bool {
		return entries0[i].NameOrId >= kind
	})
	if i0 >= len(entries0) || entries0[i0].NameOrId != kind {
		// inserting new entry & dir
		entries0 = append(entries0[:i0], append([]DirEntry{{NameOrId: kind}}, entries0[i0:]...)...)
		dirs0 = append(dirs0[:i0], append([]Dir{{}}, dirs0[i0:]...)...)
		coff.Dir.NumberOfIdEntries++
	}
	coff.Dir.DirEntries = entries0
	coff.Dir.Dirs = dirs0

	// for second level, assume ID is always increasing, so we don't have to sort
	dirs0[i0].DirEntries = append(dirs0[i0].DirEntries, DirEntry{NameOrId: uint32(id)})
	dirs0[i0].Dirs = append(dirs0[i0].Dirs, Dir{
		NumberOfIdEntries: 1,
		DirEntries:        DirEntries{LANG_ENTRY},
	})
	dirs0[i0].NumberOfIdEntries++

	// calculate preceding DirEntry leaves, to find new index in Data & DataEntries
	n := 0
	for _, dir0 := range dirs0[:i0+1] {
		n += len(dir0.DirEntries) //NOTE: assuming 1 language here; TODO: dwell deeper if more langs added
	}
	n--

	// insert new data in correct place
	coff.DataEntries = append(coff.DataEntries[:n], append([]DataEntry{{Size1: size}}, coff.DataEntries[n:]...)...)
	coff.Data = append(coff.Data[:n], append([]interface{}{data}, coff.Data[n:]...)...)
}

// Freeze fills in some important offsets in resulting file.
func (coff *Coff) Freeze() {
	leafwalker := make(chan *DirEntry)
	go func() {
		for _, dir1 := range coff.Dir.Dirs { // resource type
			for _, dir2 := range dir1.Dirs { // resource ID
				for i := range dir2.DirEntries { // resource lang
					leafwalker <- &dir2.DirEntries[i]
				}
			}
		}
	}()

	var offset, diroff uint32
	binutil.Walk(coff, func(v reflect.Value, path string) error {
		switch path {
		case "/Dir":
			coff.SectionHeader32.PointerToRawData = offset
			diroff = offset
		case "/Relocations":
			coff.SectionHeader32.PointerToRelocations = offset
			coff.SectionHeader32.SizeOfRawData = offset - diroff
		case "/Symbols":
			coff.FileHeader.PointerToSymbolTable = offset
		}
		RE := regexp.MustCompile
		const N = `\[(\d+)\]`
		m := matcher{}
		switch {
		case m.Find(path, RE("^/Dir/Dirs"+N+"$")):
			coff.Dir.DirEntries[m[0]].OffsetToData = MASK_SUBDIRECTORY | (offset - diroff)
		case m.Find(path, RE("^/Dir/Dirs"+N+"/Dirs"+N+"$")):
			coff.Dir.Dirs[m[0]].DirEntries[m[1]].OffsetToData = MASK_SUBDIRECTORY | (offset - diroff)
		case m.Find(path, RE("^/DataEntries"+N+"$")):
			direntry := <-leafwalker
			direntry.OffsetToData = offset - diroff
		case m.Find(path, RE("^/DataEntries"+N+"/OffsetToData$")):
			coff.Relocations[m[0]].RVA = offset - diroff
		case m.Find(path, RE("^/Data"+N+"$")):
			coff.DataEntries[m[0]].OffsetToData = offset - diroff
		}

		if binutil.Plain(v.Kind()) {
			offset += uint32(binary.Size(v.Interface())) // TODO: change to v.Type().Size() ?
			return nil
		}
		vv, ok := v.Interface().(binutil.SizedReader)
		if ok {
			offset += uint32(vv.Size())
			return binutil.WALK_SKIP
		}
		return nil
	})
}

func mustAtoi(s string) int {
	i, err := strconv.Atoi(s)
	if err != nil {
		panic(err)
	}
	return i
}

type matcher []int

func (m *matcher) Find(s string, re *regexp.Regexp) bool {
	subs := re.FindStringSubmatch(s)
	if subs == nil {
		return false
	}

	*m = (*m)[:0]
	for i := 1; i < len(subs); i++ {
		*m = append(*m, mustAtoi(subs[i]))
	}
	return true
}