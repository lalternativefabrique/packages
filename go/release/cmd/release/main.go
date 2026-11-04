// Command release uploads build artifacts to a public S3 bucket under
// <prefix>/<version>/ and <prefix>/latest/ with a SHA256SUMS.
//
//	AWS_ACCESS_KEY_ID=… AWS_SECRET_ACCESS_KEY=… release -endpoint https://s3.gra.io.cloud.ovh.net \
//	  -region gra -bucket orbit-prod-releases dist/*
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/lalternative/packages/go/release"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "release:", err)
		os.Exit(1)
	}
}

func run() error {
	endpoint := flag.String("endpoint", "", "S3 endpoint URL")
	region := flag.String("region", "", "S3 region")
	bucket := flag.String("bucket", "", "bucket name")
	prefix := flag.String("prefix", "", "key prefix inside the bucket")
	version := flag.String("version", "", "release version (default <UTC date>-<short sha>)")
	publicURL := flag.String("public-url", "", "public base URL (default https://<bucket>.<endpoint host>)")
	allowDirty := flag.Bool("allow-dirty", false, "allow a release off main or from a dirty tree")
	flag.Parse()

	if *endpoint == "" || *region == "" || *bucket == "" {
		return fmt.Errorf("-endpoint, -region and -bucket are required")
	}
	accessKey, secretKey := os.Getenv("AWS_ACCESS_KEY_ID"), os.Getenv("AWS_SECRET_ACCESS_KEY")
	if accessKey == "" || secretKey == "" {
		return fmt.Errorf("AWS_ACCESS_KEY_ID and AWS_SECRET_ACCESS_KEY are required")
	}
	if !*allowDirty {
		if err := requireCleanMain(); err != nil {
			return err
		}
	}
	if *version == "" {
		sha, err := git("rev-parse", "--short=12", "HEAD")
		if err != nil {
			return err
		}
		*version = time.Now().UTC().Format("20060102-1504") + "-" + sha
	}
	if *publicURL == "" {
		host := strings.TrimPrefix(strings.TrimPrefix(*endpoint, "https://"), "http://")
		*publicURL = "https://" + *bucket + "." + host
	}

	up := release.NewS3Uploader(release.S3Config{
		Endpoint: *endpoint, Region: *region, Bucket: *bucket, AccessKey: accessKey, SecretKey: secretKey,
	})
	res, err := release.Publish(context.Background(), up, release.Config{
		Prefix: *prefix, Version: *version, PublicURL: *publicURL,
	}, flag.Args())
	if err != nil {
		return err
	}

	fmt.Printf("published %s\n", res.Version)
	for _, a := range res.Artifacts {
		fmt.Printf("%s  %s\n  %s\n  %s\n", a.SHA256, a.Name, a.URL, a.LatestURL)
	}
	fmt.Println(res.SumsURL)
	return nil
}

func requireCleanMain() error {
	branch, err := git("rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return err
	}
	if branch != "main" {
		return fmt.Errorf("release from main only (on %s)", branch)
	}
	status, err := git("status", "--porcelain")
	if err != nil {
		return err
	}
	if status != "" {
		return fmt.Errorf("working tree is not clean")
	}
	return nil
}

func git(args ...string) (string, error) {
	out, err := exec.Command("git", args...).Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return strings.TrimSpace(string(out)), nil
}
