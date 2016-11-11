package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"github.com/fatih/color"
	"github.com/ibmjstart/swiftlygo/auth"
	"github.com/ibmjstart/swiftlygo/slo"
	"os"
	"os/signal"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// Helper functions to ANSI colorize output
var Cyan (func(string, ...interface{}) string) = color.New(color.FgCyan, color.Bold).SprintfFunc()
var Green (func(string, ...interface{}) string) = color.New(color.FgGreen, color.Bold).SprintfFunc()
var Red (func(string, ...interface{}) string) = color.New(color.FgRed, color.Bold).SprintfFunc()
var Yellow (func(string, ...interface{}) string) = color.New(color.FgYellow, color.Bold).SprintfFunc()

// clearLine is the ANSI escape code for clearing a terminal line.
var clearLine string = "\033[2K"

// upLine is the ANSI escape code for moving the cursor up a line in the terminal.
var upLine string = "\033[1A"

/*
Exit code handling borrowed from the brilliant insight of this playground:
https://play.golang.org/p/4tyWwhcX0-
Authored by marcio and referenced in his answer here:
http://stackoverflow.com/questions/27629380/how-to-exit-a-go-program-honoring-deferred-calls
*/
type Exit struct{ Code int }

// handleExit catches any panics and checks whether they are deliberate attempts to exit,
// allowing them to panic normally if they are not
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
	var (
		path, tenant, userName,
		apiKey, authURL, domain,
		container, objectName,
		hashFile, excludedChunks string
		chunkSize            uint
		maxUploads           int
		hashesOut, hashes    map[string]string
		onlyMissing, noColor bool
		serversideChunks     []string
	)
	defer handleExit() // prep exit handler
	flag.StringVar(&userName, "user", "", "`username` from OpenStack Object Storage credentials")
	flag.StringVar(&apiKey, "p", "", "`password` from OpenStack Object Storage credentials")
	flag.StringVar(&authURL, "url", "", "`auth_url` from OpenStack Object Storage credentials. IMPORTANT: Append \"/vX\" to the end of this URL where X is your swift authentication version")
	flag.StringVar(&domain, "d", "", "[auth v3 only] `domainName` from OpenStack Object Storage credentials")
	flag.StringVar(&container, "c", "", "`name` of the container in object storage in which you want to store the data")
	flag.StringVar(&objectName, "o", "", "`name` of the object in object storage in which you want to store the data")
	flag.StringVar(&tenant, "t", "", "[auth v2 only] `name` from OpenStack Object Storage credentials")
	flag.StringVar(&path, "f", "", "the `path` to the local file being uploaded")
	flag.StringVar(&excludedChunks, "e", "", "[optional] `comma-separated-list` (no spaces) of chunks to skip uploading. WARNING: This WILL cause SLO Manifest Uploads to fail.")
	flag.StringVar(&hashFile, "h", "", "[optional] `filename` of a hash json file saved by this utility on a previous run. This can shortcut hashing data.")
	flag.UintVar(&chunkSize, "z", 1e9, "the `size` of each file chunk being uploaded")
	flag.IntVar(&maxUploads, "j", runtime.NumCPU(), "the number of parallel uploads that you want, at maximum.")
	flag.BoolVar(&onlyMissing, "only-missing", false, "only upload file chunks that are not already in object storage (uses name matching)")
	flag.BoolVar(&noColor, "no-color", false, "disable colorization on output (also disables ANSI line clearing)")
	flag.Parse()

	// configure colorization
	color.NoColor = noColor
	if noColor {
		clearLine = ""
		upLine = ""
	}

	// check required parameters
	if path == "" || userName == "" || apiKey == "" || authURL == "" || container == "" || objectName == "" {
		fmt.Fprintln(os.Stderr, Red("Missing required arguments, see `"+os.Args[0]+" --help` for details"))
		panic(Exit{2})
	}

	// Authenticate
	connection, err := auth.Authenticate(userName, apiKey, authURL, domain, tenant)
	if err != nil {
		fmt.Fprintf(os.Stderr, Red("Authentication error: %s", err))
		panic(Exit{2})
	}

	// Prepare file for upload
	file, err := os.Open(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, Red("Unable to open file %s: %s\n", path, err))
		panic(Exit{2})
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		fmt.Fprintf(os.Stderr, Red("Unable to stat file %s: %s\n", path, err))
		panic(Exit{2})
	}
	fmt.Println(Green("Source file opened successfully"))

	// Initialize the hashes map regardless of whether the file is provided; it's used to write the file later
	hashes = make(map[string]string)
	if hashFile != "" {
		// Attempt to open hash file
		hashesSource, err := os.Open(hashFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, Red("Unable to hash json file %s: %s\n", hashFile, err))
			panic(Exit{2})
		}
		defer hashesSource.Close()
		hashinfo, err := hashesSource.Stat()
		if err != nil {
			fmt.Fprintf(os.Stderr, Red("Unable to stat file %s: %s\n", hashFile, err))
			panic(Exit{2})
		}
		hashdata := make([]byte, hashinfo.Size())
		_, err = hashesSource.Read(hashdata)
		if err != nil {
			fmt.Fprintf(os.Stderr, Red("Unable to read file %s: %s\n", hashFile, err))
			panic(Exit{2})
		}
		err = json.Unmarshal(hashdata, &hashes)
		if err != nil {
			fmt.Fprintf(os.Stderr, Red("Problem converting file %s to golang map: %s\n", hashFile, err))
			panic(Exit{2})
		}
		fmt.Println(Green("Hash file opened successfully"))
	}

	// set up the list of missing chunks
	if onlyMissing {
		serversideChunks, err = connection.FileNames(container)
		if err != nil {
			fmt.Fprintf(os.Stderr, Red("Problem getting existing chunks names from object storage: %s\n", err))
			panic(Exit{2})
		}
	} else {
		serversideChunks = make([]string, 0)
	}

	// Parse chunk exclusion list
	var excludedChunkNumber []uint = make([]uint, 0)
	if excludedChunks != "" {
		numbers := strings.Split(excludedChunks, ",")
		for _, number := range numbers {
			realNumber, err := strconv.Atoi(number)
			if err != nil {
				fmt.Fprintln(os.Stderr, Red("Error parsing exclusion list at %s: %s", number, err))
				os.Exit(1)
			}
			excludedChunkNumber = append(excludedChunkNumber, uint(realNumber))
		}
	}

	// Define a function to associate hashes with their chunks
	hashAssociate := func(chunk slo.FileChunk) (slo.FileChunk, error) {
		if len(hashes) >= 1 {
			if hash, ok := hashes[chunk.Path()]; ok {
				chunk.Hash = hash
			}
			return chunk, nil
		}
		return chunk, nil
	}
	// Define a function that prints manifest names when the pass through
	printManifest := func(chunk slo.FileChunk) (slo.FileChunk, error) {
		fmt.Printf(Cyan("Uploading manifest: %s\n", chunk.Path()))
		return chunk, nil
	}
	///////////////////////
	// Execute the Pipeline
	///////////////////////
	errors := make(chan error, 100)
	chunks, numberChunks := slo.BuildChunks(uint(info.Size()), chunkSize)
	chunks = slo.ObjectNamer(chunks, objectName+"-chunk-%04[1]d-size-%[2]d")
	chunks = slo.Containerizer(chunks, container)
	// Filter out excluded chunks before reading and hashing data
	excluded, chunks := slo.Separate(chunks, errors, func(chunk slo.FileChunk) (bool, error) {
		for _, chunkNumber := range excludedChunkNumber {
			if chunkNumber == chunk.Number {
				return true, nil
			}
		}
		return false, nil
	})
	// Attach known hashes to excluded chunks
	excluded = slo.Map(excluded, errors, hashAssociate)
	// Excluded chunks will be join the others after the upload process

	// Read data for non-excluded chunks
	chunks = slo.ReadData(chunks, errors, file)
	// Separate out chunks that should not be hashed (ideally b/c they have already been hashed)
	nohash, chunks := slo.Separate(chunks, errors, func(chunk slo.FileChunk) (bool, error) {
		_, ok := hashes[chunk.Path()]
		return ok, nil
	})
	// Attach known hashes (should shortcut hash computation if any are known)
	nohash = slo.Map(nohash, errors, hashAssociate)
	// Perform the hash on those that need it
	chunks = slo.HashData(chunks, errors)
	// Bring all of the hashed chunks back together
	chunks = slo.Join(nohash, chunks)
	chunks, hashCounts := slo.Counter(chunks)
	chunks, jsonIn := slo.Fork(chunks)
	// Separate out chunks that should not be uploaded
	noupload, chunks := slo.Separate(chunks, errors, func(chunk slo.FileChunk) (bool, error) {
		for _, chunkName := range serversideChunks {
			if chunkName == chunk.Object {
				return true, nil
			}
		}
		return false, nil
	})

	// Perform upload
	uploadStreams := slo.Divide(chunks, uint(maxUploads))
	doneStreams := make([]<-chan slo.FileChunk, maxUploads)
	for index, stream := range uploadStreams {
		doneStreams[index] = slo.UploadData(stream, errors, connection, time.Second)
	}
	chunks = slo.Join(doneStreams...)
	chunks = slo.Map(chunks, errors, func(chunk slo.FileChunk) (slo.FileChunk, error) {
		chunk.Data = nil // Discard data to allow it to be garbage-collected
		return chunk, nil
	})
	chunks, uploadCounts := slo.Counter(chunks)

	// Bring all of the chunks back together
	chunks = slo.Join(chunks, noupload, excluded)
	// Build manifest layer 1
	manifests := slo.ManifestBuilder(chunks, errors)
	manifests = slo.ObjectNamer(manifests, objectName+"-manifest-%04[1]d")
	manifests = slo.Containerizer(manifests, container)
	// Upload manifest layer 1
	manifests = slo.Map(manifests, errors, printManifest)
	manifests = slo.UploadManifests(manifests, errors, connection)
	// Build top-level manifest out of layer 1
	topManifests := slo.ManifestBuilder(manifests, errors)
	topManifests = slo.ObjectNamer(topManifests, objectName)
	topManifests = slo.Containerizer(topManifests, container)
	// Upload top-level manifest
	topManifests = slo.Map(topManifests, errors, printManifest)
	topManifests = slo.UploadManifests(topManifests, errors, connection)

	//////////////////////////
	// Process Pipeline Output
	//////////////////////////

	// Handle saving hashes in case a retry is needed
	hashesOut = make(map[string]string)
	go func() {
		for j := range jsonIn {
			hashesOut[j.Path()] = j.Hash
		}
	}()
	saveHashFile := func() {
		fmt.Println(Yellow("\nAttempting hash file write"))
		file, err := os.Create(objectName + strings.Replace(fmt.Sprintf("-%s.json", time.Now()), " ", "-", -1))
		if err != nil {
			fmt.Fprintf(os.Stderr, Yellow("Error opening data backup file: %s", err))
		}
		defer file.Close()
		data, err := json.Marshal(hashesOut)
		if err != nil {
			fmt.Fprintf(os.Stderr, Yellow("Error data converting data to JSON: %s", err))
		}
		_, err = file.Write(data)
		if err != nil {
			fmt.Fprintf(os.Stderr, Yellow("Error writing to data backup file: %s", err))
		}
		fmt.Println(Green("Hash file %s written successfully", file.Name()))
	}
	defer saveHashFile()

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
		for man := range topManifests {
			fmt.Println(Green("Upload succeeded for %s", man.Path()))
		}
		close(errors) //closing this errors channel will signal the main goroutine to exit
		for err := range errors {
			fmt.Fprintln(os.Stderr, Yellow(err.Error()))
		}
		done <- struct{}{}
	}()

	// Print the upload counts as they come in
	go func(totalChunks uint, hashCounts, uploadCounts <-chan slo.Count) {
		var (
			hashCount, uploadCount                           slo.Count
			hashPercent, uploadPercent, hashRate, uploadRate float64
		)
		fmt.Println()
		for {
			select {
			case hashCount = <-hashCounts:
				hashPercent = float64(hashCount.Chunks) / float64(totalChunks) * 100
				hashRate = float64(hashCount.Bytes) / float64(hashCount.Elapsed.Seconds())
			case uploadCount = <-uploadCounts:
				uploadPercent = float64(uploadCount.Chunks) / float64(totalChunks) * 100
				uploadRate = float64(uploadCount.Bytes) / float64(uploadCount.Elapsed.Seconds())
			}
			fmt.Println(Cyan(upLine+clearLine+"[%s] %02.2f%% hashed (%02.2f KiB/s) %02.2f%% uploaded (%02.2f KiB/s)", time.Now().String(), hashPercent, hashRate/1024, uploadPercent, uploadRate/1024))
		}
	}(numberChunks, hashCounts, uploadCounts)

	// Drain the errors channel, this will block until the errors channel is closed above.
	select {
	case <-done:
		panic(Exit{0})
	case <-interrupt:
		panic(Exit{130})
	}
}
