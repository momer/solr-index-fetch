## Solr index fetcher
Retrieves the most recent / current Solr index version via the Solr HTTP API. This occurs concurrently, so that downloads do not block or slow down the process for large indeces.

Reading http responses does not pipe directly to an output file, so each file may potentially be held in memory at the moment. Note that when writing to output files, this memory is not duplicated to the tune of the file size, as io.Copy writes 32MB per buffer at a time, which means at any given time you may have the overhead of any 4 Index files + 128MB, along with channel etc. overhead.

## Implementation and Dependencies
This script was built with Go 1.2.1, and is suitable for retrieving the latest version of a Solr instance's index.

By default, the script will only use 1/2 of your cores for concurrency; ie: if you have 8 cores, you will get 4 workers. 

Works flawlessly with Solr 4.6.1, and should work just as well with any version of Solr that implements the same HTTP API and answers queries using the same XML response format.

## Build
    go build solr-fetch.go

## Usage
Options:
- `-l=<URL to Solr Admin page>`
- `-o=<Output Path for the downloaded Solr Index>`

Example:

    localhost:solr-fetch Mo$ ./solr-fetch -l=http://172.20.20.20:8983/solr -o=/var/lib/solr/backup
    
    2014/06/01 09:05:15 Beginning fetch of Solr index...
    2014/06/01 09:05:15 {Url:http://172.20.20.20:8983/solr/replication?command=filecontent&file=segments_d&generation=13&indexversion=1401508582278&wt=filestream StatusCode:200}
  	...

    localhost:solr-fetch Mo$ ls results/
    _8.fdt      _8.si       _8_Lucene41_0.pos
    _8.fdx      _8.tvd      _8_Lucene41_0.tim
    _8.fnm      _8.tvx      _8_Lucene41_0.tip
    _8.nvd      _8_1.del    segments_d
    _8.nvm      _8_Lucene41_0.doc

## Notes/TODO
Not implemented:
- A friendly delay between requests to the Solr server. Solr has no problem fetching these requests in development without delay, though this could change as pressure on the Solr server increases.
- Clear and retry download in the event of a failure for any N number of attempts requested by CLI.
- Health check Solr server before beginning process of document retrieval.
