# rsrc
Tool for embedding binary resources in Go programs.

http://github.com/gonutz/rsrc

# Installation
	go get github.com/gonutz/rsrc

# Usage

```
rsrc [-manifest FILE.exe.manifest] [-ico FILE.ico[,FILE2.ico...]] -o FILE.syso
  Generates a .syso file with specified resources embedded in .rsrc section,
  aimed for consumption by Go linker when building Win32 excecutables.

The generated *.syso files should get automatically recognized by 'go build'
command and linked into an executable/library, as long as there are any *.go
files in the same directory.

If you build for multiple architectures, name your rsrc files accordingly, e.g.
rsrc_386.syso and rsrc_amd64.syso to have the Go compiler use the right one
depending on the current GOOS.

The mapping of input files to their resource IDs is printed as Go code to
stdout. You can save the output to a .go file which will contain a
  var exeIDs = map[string]uint16
which you can access at runtime to retrieve the resource ID for each embedded
file.

OPTIONS:
  -arch string
    	architecture of output file - one of: 386, [EXPERIMENTAL: amd64] (default "386")
  -ico string
    	comma-separated list of paths to .ico files to embed
  -manifest string
    	path to a Windows manifest file to embed
  -o string
    	name of output COFF (.res or .syso) file (default "rsrc.syso")
```
