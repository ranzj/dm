package streamer

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/juju/errors"
	"github.com/ngaut/log"
	"github.com/pingcap/tidb-enterprise-tools/pkg/utils"
)

func collectBinlogFiles(dirpath string, firstFile string) ([]string, error) {
	files, err := readDir(dirpath)
	if err != nil {
		return nil, errors.Trace(err)
	}
	if fp := filepath.Join(dirpath, firstFile); !utils.IsFileExists(fp) {
		return nil, errors.Errorf("%s not exists", fp)
	}

	targetFiles := make([]string, 0, len(files))

	ff := parseBinlogFile(firstFile)
	if ff == nil {
		return nil, errors.Errorf("firstfile %s is invalid ", firstFile)
	}
	if !allAreDigits(ff.seq) {
		return nil, errors.Errorf("firstfile %s is invalid ", firstFile)
	}

	for _, f := range files {
		// check prefix
		if !strings.HasPrefix(f, ff.baseName) {
			log.Warnf("collecting binlog file, ignore invalid file %s", f)
			continue
		}
		// check suffix

		parsed := parseBinlogFile(f)
		if !allAreDigits(parsed.seq) {
			log.Warnf("collecting binlog file, ignore invalid file %s", f)
			continue
		}
		targetFiles = append(targetFiles, f)
	}

	return targetFiles, nil
}

func parseBinlogFile(filename string) *binlogFile {
	// chendahui: I found there will always be only one dot in the mysql binlog name.
	parts := strings.Split(filename, ".")
	if len(parts) != 2 {
		log.Warnf("filename %s not valid", filename)
		return nil
	}

	return &binlogFile{
		baseName: parts[0],
		seq:      parts[1],
	}
}

type binlogFile struct {
	baseName string
	seq      string
}

func (f *binlogFile) BiggerThan(other *binlogFile) bool {
	return f.baseName == other.baseName && f.seq > other.seq
}

func (f *binlogFile) Equal(other *binlogFile) bool {
	return f.baseName == other.baseName && f.seq == other.seq
}

func (f *binlogFile) BiggerOrEqualThan(other *binlogFile) bool {
	return f.baseName == other.baseName && f.seq >= other.seq
}

// [0-9] in string -> [48,57] in ascii
func allAreDigits(s string) bool {
	for _, r := range s {
		if r >= 48 && r <= 57 {
			continue
		}
		return false
	}
	return true
}

// readDir reads and returns all file(sorted asc) and dir names from directory f
func readDir(dirpath string) ([]string, error) {
	dir, err := os.Open(dirpath)
	if err != nil {
		return nil, errors.Trace(err)
	}
	defer dir.Close()

	names, err := dir.Readdirnames(-1)
	if err != nil {
		return nil, errors.Annotatef(err, "dir %s", dirpath)
	}

	sort.Strings(names)

	return names, nil
}