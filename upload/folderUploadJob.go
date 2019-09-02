package upload

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

	gphotos "github.com/gphotosuploader/google-photos-api-client-go/lib-gphotos"

	"github.com/gphotosuploader/gphotos-uploader-cli/datastore/completeduploads"
	"github.com/gphotosuploader/gphotos-uploader-cli/datastore/uploadurls"
	"github.com/gphotosuploader/gphotos-uploader-cli/utils/filesystem"
)

// Job represents a job to upload all photos from the specified folder
type Job struct {
	client            *gphotos.Client
	trackingService   *completeduploads.Service
	uploadURLsService *uploadurls.Service

	sourceFolder string
	options      *JobOptions
}

// JobOptions represents all the options that a job can have
type JobOptions struct {
	createAlbum       bool
	deleteAfterUpload bool
	uploadVideos      bool
	includePatterns   []string
	excludePatterns   []string
}

// NewJobOptions create a jobOptions based on the submitted / validated data
func NewJobOptions(createAlbum bool, deleteAfterUpload bool, uploadVideos bool, includePatterns []string, excludePatterns []string) *JobOptions {
	return &JobOptions{
		createAlbum:       createAlbum,
		deleteAfterUpload: deleteAfterUpload,
		uploadVideos:      uploadVideos,
		includePatterns:   includePatterns,
		excludePatterns:   excludePatterns,
	}
}

// NewFolderUploadJob creates a job based on the submitted data
func NewFolderUploadJob(client *gphotos.Client, trackingService *completeduploads.Service, uploadURLsService *uploadurls.Service, fp string, opt *JobOptions) *Job {
	return &Job{
		trackingService:   trackingService,
		uploadURLsService: uploadURLsService,
		client:            client,

		sourceFolder: fp,
		options:      opt,
	}
}

// ScanFolder uploads folder
func (job *Job) ScanFolder(uploadChan chan<- *Item) error {
	var err error

	if !filesystem.IsDir(job.sourceFolder) {
		return fmt.Errorf("%s is not a folder", job.sourceFolder)
	}

	filter := NewFilter(job.options.includePatterns, job.options.excludePatterns, job.options.uploadVideos)

	// dirs are walked depth-first.   These vars hold the active album
	// default empty album for makeAlbums.enabled = false
	errW := filepath.Walk(job.sourceFolder, func(fp string, fi os.FileInfo, errP error) error {
		// log.Printf("ScanFolder.Walk: %v, fi: %v, err: %v\n", fp, fi, err)
		if err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "error for %v: %v\n", fp, err)
			return nil
		}
		if fi == nil {
			_, _ = fmt.Fprintf(os.Stderr, "error for %v: FileInfo is nil\n", fp)
			return nil
		}

		// check if the item should be uploaded (it's a file and it's not exclude
		if !filter.IsAllowed(fp) {
			if fi.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		// only files are allowed
		if !filesystem.IsFile(fp) {
			return nil
		}

		// check completed uploads db for previous uploads
		isAlreadyUploaded, err := job.trackingService.IsAlreadyUploaded(fp)
		if err != nil {
			log.Println(err)
		} else if isAlreadyUploaded {
			log.Printf("already uploaded: %s: skipping file...\n", fp)
			return nil
		}

		// calculate Album from the folder name, we create if it's not exists
		var albumID string
		if job.options.createAlbum {
			name := filepath.Base(filepath.Dir(fp))
			albumID = getGooglePhotosAlbumID(name, job.client)
		}

		// set file upload options depending on folder upload options
		var uploadItem = &Item{
			client:          job.client,
			path:            fp,
			album:           albumID,
			deleteOnSuccess: job.options.deleteAfterUpload,
		}

		// finally, add the file upload to the queue
		uploadChan <- uploadItem

		return nil
	})
	if errW != nil {
		log.Printf("walk error [%v]", errW)
	}

	return nil
}

// getGooglePhotosAlbumID return the Id of an album with the specified name.
// If the album doesn't exist, return an empty string.
func getGooglePhotosAlbumID(name string, c *gphotos.Client) string {
	if name == "" {
		return ""
	}

	album, err := c.GetOrCreateAlbumByName(name)
	if err != nil {
		log.Printf("Album creation failed: name=%s, error=%v", name, err)
		return ""
	}
	return album.Id
}
