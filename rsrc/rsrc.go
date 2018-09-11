package rsrc

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/gonutz/rsrc/binutil"
	"github.com/gonutz/rsrc/coff"
	"github.com/gonutz/rsrc/ico"
	"github.com/gonutz/rsrc/internal"
)

// on storing icons, see: http://blogs.msdn.com/b/oldnewthing/archive/2012/07/20/10331787.aspx
type _GRPICONDIR struct {
	ico.ICONDIR
	Entries []_GRPICONDIRENTRY
}

func (group _GRPICONDIR) Size() int64 {
	return int64(binary.Size(group.ICONDIR) + len(group.Entries)*binary.Size(group.Entries[0]))
}

type _GRPICONDIRENTRY struct {
	ico.IconDirEntryCommon
	Id uint16
}

// Embed returns a map of the resource files to their final IDs in the .syso
// file.
func Embed(outPath, arch, manifestPath, icoPath string) (map[string]uint16, error) {
	ids := make(map[string]uint16)

	lastid := uint16(0)
	newid := func() uint16 {
		lastid++
		return lastid
	}

	out := coff.NewRSRC()
	err := out.Arch(arch)
	if err != nil {
		return nil, err
	}

	if manifestPath != "" {
		manifest, err := binutil.SizedOpen(manifestPath)
		if err != nil {
			return nil, fmt.Errorf("rsrc: error opening manifest file '%s': %s", manifestPath, err)
		}
		defer manifest.Close()

		id := newid()
		out.AddResource(coff.RT_MANIFEST, id, manifest)
		ids[manifestPath] = id
	}
	if icoPath != "" {
		for _, icon := range strings.Split(icoPath, ",") {
			f, iconID, err := addIcon(out, icon, newid)
			if err != nil {
				return nil, err
			}
			ids[icon] = iconID
			defer f.Close()
		}
	}

	out.Freeze()

	return ids, internal.Write(out, outPath)
}

func addIcon(out *coff.Coff, fname string, newid func() uint16) (io.Closer, uint16, error) {
	f, err := os.Open(fname)
	if err != nil {
		return nil, 0, err
	}

	icons, err := ico.DecodeHeaders(f)
	if err != nil {
		f.Close()
		return nil, 0, err
	}

	var iconID uint16
	if len(icons) > 0 {
		// RT_ICONs
		group := _GRPICONDIR{ICONDIR: ico.ICONDIR{
			Reserved: 0,
			Type:     1,
			Count:    uint16(len(icons)),
		}}
		for _, icon := range icons {
			id := newid()
			r := io.NewSectionReader(f, int64(icon.ImageOffset), int64(icon.BytesInRes))
			out.AddResource(coff.RT_ICON, id, r)
			group.Entries = append(group.Entries, _GRPICONDIRENTRY{icon.IconDirEntryCommon, id})
		}
		iconID = newid()
		out.AddResource(coff.RT_GROUP_ICON, iconID, group)
	}

	return f, iconID, nil
}
