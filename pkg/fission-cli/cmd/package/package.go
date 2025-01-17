/*
Copyright 2019 The Fission Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package _package

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/dchest/uniuri"
	"github.com/hashicorp/go-multierror"
	"github.com/mholt/archiver"
	"github.com/pkg/errors"

	fv1 "github.com/fission/fission/pkg/apis/fission.io/v1"
	"github.com/fission/fission/pkg/controller/client"
	pkgutil "github.com/fission/fission/pkg/fission-cli/cmd/package/util"
	"github.com/fission/fission/pkg/fission-cli/cmd/spec"
	spectypes "github.com/fission/fission/pkg/fission-cli/cmd/spec/types"
	"github.com/fission/fission/pkg/fission-cli/util"
	"github.com/fission/fission/pkg/utils"
)

// CreateArchive returns a fv1.Archive made from an archive .  If specFile, then
// create an archive upload spec in the specs directory; otherwise
// upload the archive using client.  noZip avoids zipping the
// includeFiles, but is ignored if there's more than one includeFile.
func CreateArchive(client *client.Client, includeFiles []string, noZip bool, specDir string, specFile string) (*fv1.Archive, error) {

	errs := &multierror.Error{}

	// check files existence
	for _, path := range includeFiles {
		// ignore http files
		if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
			continue
		}

		// Get files from inputs as number of files decide next steps
		files, err := utils.FindAllGlobs([]string{path})
		if err != nil {
			util.CheckErr(err, "finding all globs")
		}

		if len(files) == 0 {
			errs = multierror.Append(errs, errors.New(fmt.Sprintf("Error finding any files with path \"%v\"", path)))
		}
	}

	if errs.ErrorOrNil() != nil {
		return nil, errs.ErrorOrNil()
	}

	if len(specFile) > 0 {
		// create an ArchiveUploadSpec and reference it from the archive
		aus := &spectypes.ArchiveUploadSpec{
			Name:         archiveName("", includeFiles),
			IncludeGlobs: includeFiles,
		}

		// check if this AUS exists in the specs; if so, don't create a new one
		fr, err := spec.ReadSpecs(specDir)
		util.CheckErr(err, "read specs")
		if m := fr.SpecExists(aus, false, true); m != nil {
			fmt.Printf("Re-using previously created archive %v\n", m.Name)
			aus.Name = m.Name
		} else {
			// save the uploadspec
			err := spec.SpecSave(*aus, specFile)
			util.CheckErr(err, fmt.Sprintf("write spec file %v", specFile))
		}

		// create the archive object
		ar := &fv1.Archive{
			Type: fv1.ArchiveTypeUrl,
			URL:  fmt.Sprintf("%v%v", spec.ARCHIVE_URL_PREFIX, aus.Name),
		}
		return ar, nil
	}

	archivePath := makeArchiveFileIfNeeded("", includeFiles, noZip)

	ctx := context.Background()
	return pkgutil.UploadArchive(ctx, client, archivePath)
}

// Create an archive from the given list of input files, unless that
// list has only one item and that item is either a zip file or a URL.
//
// If the inputs have only one file and noZip is true, the file is
// returned as-is with no zipping.  (This is used for compatibility
// with v1 envs.)  noZip is IGNORED if there is more than one input
// file.
func makeArchiveFileIfNeeded(archiveNameHint string, archiveInput []string, noZip bool) string {

	// Unique name for the archive
	archiveName := archiveName(archiveNameHint, archiveInput)

	// Get files from inputs as number of files decide next steps
	files, err := utils.FindAllGlobs(archiveInput)
	if err != nil {
		util.CheckErr(err, "finding all globs")
	}

	// We have one file; if it's a zip file or a URL, no need to archive it
	if len(files) == 1 {
		// make sure it exists
		if _, err := os.Stat(files[0]); err != nil {
			util.CheckErr(err, fmt.Sprintf("open input file %v", files[0]))
		}

		// if it's an existing zip file OR we're not supposed to zip it, don't do anything
		if archiver.Zip.Match(files[0]) || noZip {
			return files[0]
		}

		// if it's an HTTP URL, just use the URL.
		if strings.HasPrefix(files[0], "http://") || strings.HasPrefix(files[0], "https://") {
			return files[0]
		}
	}

	// For anything else, create a new archive
	tmpDir, err := utils.GetTempDir()
	if err != nil {
		util.CheckErr(err, "create temporary archive directory")
	}

	archivePath, err := utils.MakeArchive(filepath.Join(tmpDir, archiveName), archiveInput...)
	if err != nil {
		util.CheckErr(err, "create archive file")
	}

	return archivePath
}

// Name an archive
func archiveName(givenNameHint string, includedFiles []string) string {
	if len(givenNameHint) > 0 {
		return fmt.Sprintf("%v-%v", givenNameHint, uniuri.NewLen(4))
	}
	if len(includedFiles) == 0 {
		return uniuri.NewLen(8)
	}
	return fmt.Sprintf("%v-%v", util.KubifyName(includedFiles[0]), uniuri.NewLen(4))
}

func GetFunctionsByPackage(client *client.Client, pkgName, pkgNamespace string) ([]fv1.Function, error) {
	fnList, err := client.FunctionList(pkgNamespace)
	if err != nil {
		return nil, err
	}
	fns := []fv1.Function{}
	for _, fn := range fnList {
		if fn.Spec.Package.PackageRef.Name == pkgName {
			fns = append(fns, fn)
		}
	}
	return fns, nil
}
