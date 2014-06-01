/* Script to retrieve the latest solr index data from a server */
package main

import (
  "runtime"
  "net/http"
  "net/url"
  "path"
  "path/filepath"
  "io"
  "io/ioutil"
  "encoding/xml"
  "os"
  "flag"
  "log"
)

/* Solr XML Response header types */
type ResponseHeader struct {
  Metadata []HeaderData `xml:"int"`
}

type HeaderData struct {
  Name string `xml:"name,attr"`
  Value string `xml:",innerxml"`
}

/* Solr XML Index Discovery types */
type DiscoveryResult struct {
  Header ResponseHeader `xml:"lst"`
  VersionInfo []VersionInfo `xml:"long"`
}

type VersionInfo struct {
  Name string `xml:"name,attr"`
  Value string `xml:",innerxml"`
}

/* Solr XML File Discovery types */
type FileListResult struct {
  Header ResponseHeader `xml:"lst"`
  Filesets []FileList `xml:"arr"`
}

type FileList struct {
  Name  string      `xml:"name,attr"`
  Files []IndexFile `xml:"lst"`
}

type IndexFile struct {
  Name string `xml:"str"`
  Size string `xml:"long"`
}

/* Goroutine Channel types */
type SolrIndex struct {
  Url string
  Version string
  Generation string
}

type DownloadResult struct {
  Url string
  StatusCode int
}

type Download struct {
  Name string
  Url string
  Results chan<- DownloadResult
}

var workers = runtime.NumCPU()/2

var SolrUrl,
  OutputPath,
  SuccessFile string

func (download Download) Do(outputPath string) {
  out, err := os.Create(path.Join(outputPath, download.Name))
  defer out.Close()
  if err != nil {
    log.Fatal(err)
  }

  response, err := http.Get(download.Url)
  defer response.Body.Close()
  if err != nil {
    log.Fatal(err)
  }

  _, err = io.Copy(out, response.Body)
  if err != nil {
    log.Fatal(err)
  }
  download.Results <- DownloadResult{download.Url, response.StatusCode}
}

func init() {
  defaultSuccessFile, err := filepath.Abs(filepath.Dir(os.Args[0]))
  if err != nil {
    log.Fatal(err)
  }

  const (
    defaultSolrUrl = "http://172.20.20.20:8983/solr"
    defaultOutputPath = "/var/lib/solr/data"
  )

  flag.StringVar(&SolrUrl, "l", defaultSolrUrl, "Location of the Solr server (ie: http://localhost:8983/solr)")
  flag.StringVar(&OutputPath, "o", defaultOutputPath, "Output location of the downloaded solr index")
  flag.StringVar(&SuccessFile, "s", defaultSuccessFile, "Path to the file which indicates that all the files downloaded successfully")
}

func main() {
  runtime.GOMAXPROCS(workers) // Set number of threads/workers
  flag.Parse()

  if err := os.MkdirAll(OutputPath, 0700); err != nil {
    log.Fatal("Unable to create output path")
  }

  pendingDownloads := make(chan Download, workers)
  results := make(chan DownloadResult, 1000)
  done := make(chan struct{}, workers)

  log.Println("Beginning fetch of Solr index...")
  go queueIndexFileDownloads(pendingDownloads, SolrUrl, results)
  for i := 0; i < workers; i++ {
    go downloadIndexFiles(done, OutputPath, pendingDownloads)
  }
  go awaitCompletion(done, results)
  processResults(results)
}

func queueIndexFileDownloads(pendingDownloads chan<- Download, solrUrl string, results chan<- DownloadResult) (error) {
  defer close(pendingDownloads)
  // Step 1: Get information about the Solr Index
  solrIndex, err := getLatestIndexInfo(solrUrl)
    if err != nil {
    log.Fatal("error: %+v", err)
  }
  // Step 2: Get information about the files available
  indexFiles, err := getIndexFileList(solrIndex)
  if err != nil {
    log.Fatal("error: %+v", err)
  }
  // Step 3: Get the files
  for _, file := range indexFiles {
    url, err := generateFileDownloadUrl(solrIndex, file)
    if err != nil {
      log.Fatal("Unable to create a url for: ", file)
    }

    pendingDownloads <- Download{file.Name, url, results}
  }

  return nil
}

// Step 1: Get information about the index
func getLatestIndexInfo(solrUrl string) (SolrIndex, error) {
  solrIndex := SolrIndex{Url: solrUrl}
  response, err := fetchIndexInfo(solrIndex.Url)
  if err != nil {
    log.Fatal(err)
  }

  indexDiscoveryResult, err := parseXmlIndexInfo(response)
  if err != nil {
    log.Fatal(err)
  }

  for _, metadata := range indexDiscoveryResult.Header.Metadata {
    if metadata.Name == "status" && metadata.Value != "0" {
      log.Fatal("Error, did not discover Solr Index info as expected: ", err)
    }
  }

  for _, descriptor := range indexDiscoveryResult.VersionInfo {
    switch descriptor.Name {
    case "indexversion":
      solrIndex.Version = descriptor.Value

    case "generation":
      solrIndex.Generation = descriptor.Value
    }
  }

  return solrIndex, nil
}

func fetchIndexInfo(solrUrl string) (*http.Response, error) {
  indexDiscoveryUrl, err := generateIndexDiscoveryUrl(solrUrl)
  if err != nil {
    log.Fatal(err)
  }

  response, err := http.Get(indexDiscoveryUrl)
  if err != nil {
    log.Fatal(err)
  }

  return response, nil
}

// http://172.20.20.20:8983/solr/replication?command=indexversion
func generateIndexDiscoveryUrl(solrUrl string) (string, error) {
  indexDiscoveryUrl, err := url.Parse(solrUrl)
  if err != nil {
    log.Println("generateIndexDiscoveryUrl(): Unable to parse solrUrl.")
    return "", err
  }
  indexDiscoveryUrl.Path = path.Join(indexDiscoveryUrl.Path, "replication")

  query := indexDiscoveryUrl.Query()
  query.Set("command", "indexversion")

  indexDiscoveryUrl.RawQuery = query.Encode()
  return indexDiscoveryUrl.String(), err
}

func parseXmlIndexInfo(response *http.Response) (*DiscoveryResult, error) {
  xmlIndexInfo, err := ioutil.ReadAll(response.Body)
  defer response.Body.Close()

  if err != nil {
    log.Fatal(err)
  }

  discoveryResult := DiscoveryResult{} 
  err = xml.Unmarshal([]byte(xmlIndexInfo), &discoveryResult)
  if err != nil {
    log.Fatal("error: %+v", err)
  }

  return &discoveryResult, nil
}

// Step 2: Get the index file info and return meaningful data structures
func getIndexFileList(solrIndex SolrIndex) ([]IndexFile, error) {
  indexFiles := make([]IndexFile, 0)
  response, err := fetchIndexFileData(solrIndex)
  if err != nil {
    log.Fatal(err)
  }

  fileListResult, err := parseXmlFileList(response)
  if err != nil {
    log.Fatal(err)
  }

  for _, metadata := range fileListResult.Header.Metadata {
    if metadata.Name == "status" && metadata.Value != "0" {
      log.Fatal("Error, did not discover Solr files as expected: ", err)
    }
  }

  for _, filelist := range fileListResult.Filesets {
    if filelist.Name == "filelist" {
      indexFiles = filelist.Files
    }
  }

  return indexFiles, nil
}

// http://172.20.20.20:8983/solr/replication?command=filelist&indexversion=1401508582278&generation=13
func generateFileDiscoveryUrl(solrIndex SolrIndex) (string, error) {
  fileDiscoveryUrl, err := url.Parse(solrIndex.Url)
  if err != nil {
    log.Fatal("generateFileDiscoveryUrl(): Unable to parse solrUrl.")
    return "", err
  }

  fileDiscoveryUrl.Path = path.Join(fileDiscoveryUrl.Path, "replication")

  query := fileDiscoveryUrl.Query()
  query.Set("command", "filelist")
  query.Set("indexversion", solrIndex.Version)
  query.Set("generation", solrIndex.Generation)

  fileDiscoveryUrl.RawQuery = query.Encode()

  return fileDiscoveryUrl.String(), nil
}

func parseXmlFileList(response *http.Response) (FileListResult, error) {
  xmlFileList, err := ioutil.ReadAll(response.Body)
  defer response.Body.Close()

  if err != nil {
    log.Fatal(err)
  }

  fileListResult := FileListResult{} 
  err = xml.Unmarshal([]byte(xmlFileList), &fileListResult)
  if err != nil {
    log.Fatal("error: %+v", err)
  }

  return fileListResult, nil
}

// Step 3: Download
// http://172.20.20.20:8983/solr/replication?command=filecontent&wt=filestream&indexversion=1401508582278&generation=13&file=segments_d
func fetchIndexFileData(solrIndex SolrIndex) (*http.Response, error) {
  fileDiscoveryUrl, err := generateFileDiscoveryUrl(solrIndex)
  if err != nil {
    log.Fatal(err)
  }

  response, err := http.Get(fileDiscoveryUrl)
  if err != nil {
    log.Fatal(err)
  }

  return response, nil
}

func generateFileDownloadUrl(solrIndex SolrIndex, file IndexFile) (string, error) {
  downloadUrl, err := url.Parse(solrIndex.Url)
  if err != nil {
    log.Fatal(err)
  }
  downloadUrl.Path = path.Join(downloadUrl.Path, "replication")

  query := downloadUrl.Query()
  query.Set("command", "filecontent")
  query.Set("wt", "filestream")
  query.Set("indexversion", solrIndex.Version)
  query.Set("generation", solrIndex.Generation)
  query.Set("file", file.Name)

  downloadUrl.RawQuery = query.Encode()

  return downloadUrl.String(), nil
}

// Channel stuffs
func downloadIndexFiles(done chan<- struct{}, outputPath string, pendingDownloads <-chan Download) {
  for download := range pendingDownloads {
    download.Do(outputPath)
  }
  done <- struct{}{}
}

func awaitCompletion(done <-chan struct{}, results chan DownloadResult) {
  defer close(results)

  for i := 0; i < workers; i++ {
      <-done
  }
}

func processResults(results <-chan DownloadResult) {
  for result := range results {
      log.Printf("%+v\n", result)
  }
}