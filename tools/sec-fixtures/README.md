# sec-fixtures

This program downloads the SEC files in `testdata/sec/`, keeps only the parts that the tests use, and writes them at their paths on `www.sec.gov` and `data.sec.gov`. It also builds a small bulk archive, `companyfacts.zip`, from the trimmed files.

The companies are NVDA, AAPL, KO, GRMN and ADM. ADM has 10-K/A and 10-Q/A filings with XBRL facts.

To download the files again, run this command from this folder. Put your name and your email address in the User-Agent, because SEC refuses requests without them:

```sh
PRICEDIN_SEC_USER_AGENT='Your Name you@example.com' go run . -out ../../testdata/sec -index-date 2026-08-26
```

The program sends about 20 requests, one every 200 ms. New downloads change the fixtures, so update the expected values in `server/internal/sec` after a run.

The list of kept concepts in `main.go` must stay equal to `sec.Concepts()`. A test in `server/internal/sec` fails when a fixture holds another concept.
