## Solr index fetcher
This script was built with Go 1.2.1, and is suitable for retrieving the latest version of a Solr instance's index.

By default, the script will only use 1/2 of your cores for concurrency; ie: if you have 8 cores, you will get 4 workers. 
