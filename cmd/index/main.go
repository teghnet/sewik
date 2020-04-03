// This example demonstrates indexing documents using the esutil.BulkIndexer helper.
//
// You can configure the settings with command line flags:
//
//     go run indexer.go --workers=8 --count=100000 --flush=1000000
//
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"runtime"
	"strings"
	"sync/atomic"
	"time"

	"github.com/cenkalti/backoff"
	"github.com/dustin/go-humanize"

	"github.com/elastic/go-elasticsearch/v8"
	"github.com/elastic/go-elasticsearch/v8/esapi"
	"github.com/elastic/go-elasticsearch/v8/esutil"

	"sewik/pkg/sewik"
	"sewik/pkg/sys"
)

var (
	indexName  string
	numWorkers int
	flushBytes int
	numItems   int
	filenames  []string
	statsOn    bool
)

func init() {
	flag.StringVar(&indexName, "index", "idx", "Index name")
	flag.IntVar(&numWorkers, "workers", runtime.NumCPU()/2, "Number of indexer workers")
	flag.IntVar(&flushBytes, "flush", 5e+6, "Flush threshold in bytes")
	flag.IntVar(&numItems, "count", 10000, "Number of documents to generate")
	flag.BoolVar(&statsOn, "stats", false, "should the stats be on")

	flag.Parse()

	sys.StatsOn = statsOn
	filenames = flag.Args()

	rand.Seed(time.Now().UnixNano())
}

func main() {
	log.SetFlags(0)

	var (
		countSuccessful uint64

		res *esapi.Response
		err error
	)

	log.Printf(
		"\x1b[1mBulkIndexer\x1b[0m: documents [%s] workers [%d] flush [%s] index: %s",
		humanize.Comma(int64(numItems)), numWorkers, humanize.Bytes(uint64(flushBytes)), indexName)
	log.Println(strings.Repeat("▁", 65))

	// >>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>
	// Use a third-party package for implementing the backoff function
	retryBackoff := backoff.NewExponentialBackOff()
	// <<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<

	// >>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>
	// Create the Elasticsearch client
	// NOTE: For optimal performance, consider using a third-party HTTP transport package.
	//       See an example in the "benchmarks" folder.
	es, err := elasticsearch.NewClient(elasticsearch.Config{
		// Retry on 429 TooManyRequests statuses
		//
		RetryOnStatus: []int{502, 503, 504, 429},

		// Configure the backoff function
		//
		RetryBackoff: func(i int) time.Duration {
			if i == 1 {
				retryBackoff.Reset()
			}
			return retryBackoff.NextBackOff()
		},

		// Retry up to 5 attempts
		//
		MaxRetries: 5,
	})
	if err != nil {
		log.Fatalf("Error creating the client: %s", err)
	}
	// <<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<

	// >>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>
	//
	// Create the BulkIndexer
	//
	// NOTE: For optimal performance, consider using a third-party JSON decoding package.
	//       See an example in the "benchmarks" folder.
	//
	bi, err := esutil.NewBulkIndexer(esutil.BulkIndexerConfig{
		Index:         indexName,        // The default index name
		Client:        es,               // The Elasticsearch client
		NumWorkers:    numWorkers,       // The number of worker goroutines
		FlushBytes:    flushBytes,       // The flush threshold in bytes
		FlushInterval: 30 * time.Second, // The periodic flush interval
	})
	if err != nil {
		log.Fatalf("Error creating the indexer: %s", err)
	}
	// <<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<

	// >>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>
	// Re-create the index
	if res, err = es.Indices.Delete([]string{indexName}, es.Indices.Delete.WithIgnoreUnavailable(true)); err != nil || res.IsError() {
		log.Fatalf("Cannot delete index: %s", err)
	}
	res.Body.Close()
	res, err = es.Indices.Create(indexName, es.Indices.Create.WithBody(strings.NewReader(`{
	"settings":{"index": {"number_of_replicas": 0}}
	}`)))
	if err != nil {
		log.Fatalf("Cannot create index: %s", err)
	}
	if res.IsError() {
		log.Fatalf("Cannot create index: %s", res)
	}
	res.Body.Close()
	// <<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<

	start := time.Now().UTC()

	for d := range sewik.Docs("ZDARZENIE", sys.Filenames(filenames, 100), numWorkers, (numItems+1)*numWorkers) {
		b := fmt.Sprintf(`{"_file":"%s",%s}`, d.Source, d.Body)
		err = bi.Add(
			context.Background(),
			esutil.BulkIndexerItem{
				// Action field configures the operation to perform (index, create, delete, update)
				Action: "index",

				// DocumentID is the (optional) document ID
				DocumentID: d.ID,

				// Body is an `io.Reader` with the payload
				Body: strings.NewReader(b),

				// OnSuccess is called for each successful operation
				OnSuccess: func(ctx context.Context, item esutil.BulkIndexerItem, res esutil.BulkIndexerResponseItem) {
					atomic.AddUint64(&countSuccessful, 1)
				},

				// OnFailure is called for each failed operation
				OnFailure: func(ctx context.Context, item esutil.BulkIndexerItem, res esutil.BulkIndexerResponseItem, err error) {
					if err != nil {
						log.Printf("ERROR: [%s] %s %s", item.DocumentID, err, d.Source)
						fmt.Printf(`{"err":"%s","itemId":"%s","doc":%s},`+"\n", err, item.DocumentID, b)
					} else {
						log.Printf("ERROR: [%s] %s: %s %s", item.DocumentID, res.Error.Type, res.Error.Reason, d.Source)
						fmt.Printf(`{"err":"%s","reason":"%s","itemId":"%s","doc":%s},`+"\n", res.Error.Type, res.Error.Reason, item.DocumentID, b)
					}
				},
			},
		)
		if err != nil {
			log.Fatalf("Unexpected error: %s [%s] %s", err, d.ID, d.Source)
		}
	}

	// >>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>
	// Close the indexer
	if err := bi.Close(context.Background()); err != nil {
		log.Fatalf("Unexpected error: %s", err)
	}
	// <<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<

	biStats := bi.Stats()

	log.Println(strings.Repeat("▔", 65))

	dur := time.Since(start)

	if biStats.NumFailed > 0 {
		log.Fatalf(
			"Indexed [%s] documents with [%s] errors in %s (%s docs/sec)",
			humanize.Comma(int64(biStats.NumFlushed)),
			humanize.Comma(int64(biStats.NumFailed)),
			dur.Truncate(time.Millisecond),
			humanize.Comma(int64(1000.0/float64(dur/time.Millisecond)*float64(biStats.NumFlushed))),
		)
	} else {
		log.Printf(
			"Sucessfuly indexed [%s] documents in %s (%s docs/sec)",
			humanize.Comma(int64(biStats.NumFlushed)),
			dur.Truncate(time.Millisecond),
			humanize.Comma(int64(1000.0/float64(dur/time.Millisecond)*float64(biStats.NumFlushed))),
		)
	}
}
