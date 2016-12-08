package main

import (
	"flag"
	"fmt"
	"github.com/fatih/color"
	"github.com/ibmjstart/swiftlygo/auth"
	"github.com/ibmjstart/swiftlygo/pipeline"
	"github.com/ncw/swift"
	"github.com/pkg/profile"
	"os"
	"os/signal"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// Helper functions to ANSI colorize output
var (
	Cyan (func(string, ...interface{}) string) = color.New(color.FgCyan,
		color.Bold).SprintfFunc()
	Green (func(string, ...interface{}) string) = color.New(color.FgGreen,
		color.Bold).SprintfFunc()
	Red (func(string, ...interface{}) string) = color.New(color.FgRed,
		color.Bold).SprintfFunc()
	Yellow (func(string, ...interface{}) string) = color.New(color.FgYellow,
		color.Bold).SprintfFunc()
)

/*
Exit code handling borrowed from the brilliant insight of this playground:
https://play.golang.org/p/4tyWwhcX0-
Authored by marcio and referenced in his answer here:
http://stackoverflow.com/questions/27629380/how-to-exit-a-go-program-honoring-deferred-calls
*/
type Exit struct{ Code int }

// handleExit catches any panics and checks whether they are deliberate
// attempts to exit, allowing them to panic normally if they are not
func handleExit() {
	if e := recover(); e != nil {
		if exit, ok := e.(Exit); ok == true {
			fmt.Fprintln(os.Stderr, Red("Program exited"))
			os.Exit(exit.Code)
		}
		panic(e) // not an Exit, bubble up
	}
}

func main() {
	defer handleExit() // prep exit handler
	var (
		path, tenant, userName,
		apiKey, authURL, domain,
		container, objectName,
		excludedChunks string
		chunkSize                           uint
		maxUploads                          int
		onlyMissing, noColor, memoryProfile bool
		serversideChunks                    []swift.Object
	)
	flag.StringVar(&userName, "user", "",
		"`username` from OpenStack Object Storage credentials")
	flag.StringVar(&apiKey, "p", "",
		"`password` from OpenStack Object Storage credentials")
	flag.StringVar(&authURL, "url", "",
		"`auth_url` from OpenStack Object Storage credentials. IMPORTANT: Append \"/vX\" to the end of this URL where X is your swift authentication version")
	flag.StringVar(&domain, "d", "",
		"[auth v3 only] `domainName` from OpenStack Object Storage credentials")
	flag.StringVar(&container, "c", "",
		"`name` of the container in object storage in which you want to store the data")
	flag.StringVar(&objectName, "o", "",
		"`name` of the object in object storage in which you want to store the data")
	flag.StringVar(&tenant, "t", "",
		"[auth v2 only] `name` from OpenStack Object Storage credentials")
	flag.StringVar(&path, "f", "",
		"the `path` to the local file being uploaded")
	flag.StringVar(&excludedChunks, "e", "",
		"[optional] `comma-separated-list` (no spaces) of chunks to skip uploading. WARNING: This WILL cause SLO Manifest Uploads to fail.")
	flag.UintVar(&chunkSize, "z", 1e9,
		"the `size` of each file chunk being uploaded")
	flag.IntVar(&maxUploads, "j", runtime.NumCPU(),
		"the number of parallel uploads that you want, at maximum.")
	flag.BoolVar(&onlyMissing, "only-missing", false,
		"only upload file chunks that are not already in object storage (uses name matching)")
	flag.BoolVar(&noColor, "no-color", false,
		"disable colorization on output")
	flag.BoolVar(&memoryProfile, "memprof", false,
		"enable memory profiling for this upload")
	flag.Parse()

	// configure colorization
	color.NoColor = noColor

	// check required parameters
	if path == "" || userName == "" || apiKey == "" || authURL == "" ||
		container == "" || objectName == "" {

		fmt.Fprintln(os.Stderr, Red("Missing required arguments, see `"+
			os.Args[0]+" --help` for details"))
		panic(Exit{2})
	}

	// enable memory profiling, if required
	if memoryProfile {
		defer profile.Start(profile.MemProfile).Stop()
	}

	// Authenticate
	connection, err := auth.Authenticate(userName, apiKey, authURL, domain,
		tenant)
	if err != nil {
		fmt.Fprintf(os.Stderr, Red("Authentication error: %s", err))
		panic(Exit{2})
	}

	// Prepare file for upload
	file, err := os.Open(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, Red("Unable to open file %s: %s\n",
			path, err))
		panic(Exit{2})
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		fmt.Fprintf(os.Stderr, Red("Unable to stat file %s: %s\n",
			path, err))
		panic(Exit{2})
	}
	fmt.Println(Green("Source file opened successfully"))

	fmt.Println(Green("Uploading with %d parallel uploads", maxUploads))
	// set up the list of missing chunks
	if onlyMissing {
		serversideChunks, err = connection.Objects(container)

		if err != nil {
			fmt.Fprintf(os.Stderr, Red("Problem getting existing"+
				" chunks names from object storage: %s\n", err))
			panic(Exit{2})
		}
	} else {
		serversideChunks = make([]swift.Object, 0)
	}

	// Parse chunk exclusion list
	var excludedChunkNumber []uint = make([]uint, 0)
	if excludedChunks != "" {
		numbers := strings.Split(excludedChunks, ",")
		for _, number := range numbers {
			realNumber, err := strconv.Atoi(number)
			if err != nil {
				fmt.Fprintln(os.Stderr,
					Red("Error parsing exclusion list at"+
						" %s: %s", number, err))
				os.Exit(1)
			}
			excludedChunkNumber = append(excludedChunkNumber,
				uint(realNumber))
		}
	}

	// Define a function to associate hashes with their chunks
	hashAssociate := func(chunk pipeline.FileChunk) (pipeline.FileChunk, error) {
		for _, serverObject := range serversideChunks {
			if serverObject.Name == chunk.Object {
				chunk.Hash = serverObject.Hash
				return chunk, nil
			}
		}
		return chunk, nil
	}
	// Define a function that prints manifest names when the pass through
	printManifest := func(chunk pipeline.FileChunk) (pipeline.FileChunk, error) {
		fmt.Printf(Cyan("Uploading manifest: %s\n", chunk.Path()))
		return chunk, nil
	}
	///////////////////////
	// Execute the Pipeline
	///////////////////////
	errors := make(chan error)
	fileSize := uint(info.Size())
	chunks, numberChunks := pipeline.BuildChunks(fileSize, chunkSize)
	chunks = pipeline.ObjectNamer(chunks, errors,
		objectName+"-chunk-%04[1]d-size-%[2]d")
	chunks = pipeline.Containerizer(chunks, errors, container)
	// Filter out excluded chunks before reading and hashing data
	excluded, chunks := pipeline.Separate(chunks, errors,
		func(chunk pipeline.FileChunk) (bool, error) {
			for _, chunkNumber := range excludedChunkNumber {
				if chunkNumber == chunk.Number {
					return true, nil
				}
			}
			return false, nil
		})
	// Separate out chunks that should not be uploaded
	noupload, chunks := pipeline.Separate(chunks, errors,
		func(chunk pipeline.FileChunk) (bool, error) {
			for _, serverObject := range serversideChunks {
				if serverObject.Name == chunk.Object {
					return true, nil
				}
			}
			return false, nil
		})

	// Handle finding hashes for nonuploaded chunks
	uploadSkipped := pipeline.Join(excluded, noupload)
	uploadSkipped = pipeline.Map(uploadSkipped, errors, hashAssociate)

	// Perform upload
	uploadStreams := pipeline.Divide(chunks, uint(maxUploads))
	doneStreams := make([]<-chan pipeline.FileChunk, maxUploads)
	for index, stream := range uploadStreams {
		doneStreams[index] = pipeline.ReadHashAndUpload(stream, errors,
			file, connection)
	}
	chunks = pipeline.Join(doneStreams...)
	chunks, uploadCounts := pipeline.Counter(chunks)

	// Bring all of the chunks back together
	chunks = pipeline.Join(chunks, uploadSkipped)
	// Build manifest layer 1
	manifests := pipeline.ManifestBuilder(chunks, errors)
	manifests = pipeline.ObjectNamer(manifests, errors,
		objectName+"-manifest-%04[1]d")
	manifests = pipeline.Containerizer(manifests, errors, container)
	// Upload manifest layer 1
	manifests = pipeline.Map(manifests, errors, printManifest)
	manifests = pipeline.UploadManifests(manifests, errors, connection)
	// Build top-level manifest out of layer 1
	topManifests := pipeline.ManifestBuilder(manifests, errors)
	topManifests = pipeline.ObjectNamer(topManifests, errors, objectName)
	topManifests = pipeline.Containerizer(topManifests, errors, container)
	// Upload top-level manifest
	topManifests = pipeline.Map(topManifests, errors, printManifest)
	topManifests = pipeline.UploadManifests(topManifests, errors, connection)

	//////////////////////////
	// Process Pipeline Output
	//////////////////////////

	// Handle save on SIGINT
	interrupt, done := make(chan struct{}), make(chan struct{})
	go func(quit chan struct{}) {
		sigint := make(chan os.Signal)
		signal.Notify(sigint, os.Interrupt)
		<-sigint
		quit <- struct{}{}
	}(interrupt)

	// Print information about the top-level manifest
	go func() {
		for _ = range topManifests {
			fmt.Println(Green("Manifest uploads succeeded"))
		}
		close(errors) // signal the main goroutine to exit
	}()

	// Drain all errors and signal termination
	go func() {
		for err := range errors {
			fmt.Fprintln(os.Stderr, Yellow(err.Error()))
		}
		done <- struct{}{}
	}()

	// Print the upload counts as they come in
	go func(totalChunks, fileSize uint, uploadCounts <-chan pipeline.Count) {
		fmt.Println("The upload is starting. A status message " +
			"will be printed after each chunk is uploaded.\n" +
			"This can take some time. The time remaining and " +
			"transfer rates are rough estimates\nthat " +
			"grow more accurate as the upload progresses.")
		var (
			uploadCount               pipeline.Count
			uploadPercent, uploadRate float64
			timeRemaining             time.Duration
		)
		fmt.Println()
		for uploadCount = range uploadCounts {
			uploadPercent = float64(uploadCount.Chunks) /
				float64(totalChunks) * 100
			uploadRate = float64(uploadCount.Bytes) /
				float64(uploadCount.Elapsed.Seconds())
			timeRemaining = time.Second * time.Duration(float64(fileSize-uploadCount.Bytes)/uploadRate)
			fmt.Println("[" + time.Now().String() + "]" +
				Cyan(" %02.2f%% uploaded (%02.2f KiB/s) ~%s remaining",
					uploadPercent, uploadRate/1024, timeRemaining))
		}
	}(numberChunks, fileSize, uploadCounts)

	// exit cleanly or uncleanly depending on which channel we hear from
	select {
	case <-done:
		panic(Exit{0})
	case <-interrupt: // SIGINT
		panic(Exit{130})
	}
}
