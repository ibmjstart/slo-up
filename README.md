# slo-up


[![standard-readme compliant](https://img.shields.io/badge/standard--readme-OK-green.svg?style=flat-square)](https://github.com/RichardLitt/standard-readme)

> Tool for uploading massive data into OpenStack Object Storage

This utility is an example of using the [swiftlygo pipeline api](https://github.com/ibmjstart/swiftlygo) to build a data uploader for
OpenStack Object Storage.

## Table of Contents

- [Install](#install)
- [Usage](#usage)
- [Contribute](#contribute)
- [License](#license)

## Install

```
go get github.com/ibmjstart/slo-up
```

## Usage

### Upload to IBM Bluemix

`slo-up` makes it easy to upload data into the Object Storage service on Bluemix. Navigate to the Object Storage instanct that you would like to use in the Bluemix web user interface and find the "Service Credentials" for your Object Storage instance. It should look like this:

```json
{
  "auth_url": "https://identity.open.softlayer.com",
  "project": "project_string",
  "projectId": "project_id",
  "region": "dallas",
  "userId": "user_id",
  "username": "user_name",
  "password": "password",
  "domainId": "domain_id",
  "domainName": "domain_name",
  "role": "admin"
}
```

To upload a local file to a container (called `container_name`) in this object store with an SLO named `object_name`, you would invoke `slo-up` as follows:
```
slo-up -url https://identity.open.softlayer.com/v3 -user user_name -p password -d domain_name -c container_name -o object_name -f path/to/local/file
```
Note that we had to append `/v3` to the authentication URL.

### Authentication Flags

The flags that you need to give `slo-up` vary with the authentication version of the Object Storage instance that you're trying to
upload to. If you don't know the authentication version, the credentials that you are provided with should give you a clue.

```
slo-up -url <auth-url> -user <username> -p <password> ...upload-flags...
```

The password is sometimes referred to as an API key.
SoftLayer Object Storage uses this Authentication method.

#### Auth V2

For services supporting Auth V2, you will additionally need to provide a tenant id.
```
slo-up ...auth-flags... -t <tenant-id> ...upload-flags...
```

#### Auth V3

For services supporting Auth V3, you will additionally need to provide a domain id. 
```
slo-up ...auth-flags... -d <domain-name> ...upload-flags...
```

IBM Bluemix uses this type of authentication.

### Upload Flags

The other flags exist to provide upload options:
 - `-c <container>` The name of the container in object storage into which you are uploading data. The container must already exist.
 - `-o <object>` The name of the object in object storage into which you are uploading data. Anything in the targeted container with this name will be overwritten.
 - `-f <path>` The local file that you are uploading.
 - `-z size` Optionally choose the size of each chunk of your file. Defaults to 10^9 bytes.
 - `-e <comma-separated-list>` Optional list of chunk numbers to skip reading. Useful if some file sections are unreadable due to hard drive failure.
 - `-only-missing` Optional flag that causes uploader to only upload chunks that are not already in object storage. This is determined by checking the names of existing file chunks, and can have false positives (though it's unlikely unless you follow the `slo-up` file chunk naming convention).
 - `-memprof` Enables memory profiling for the upload. Useful mainly for debugging.
 - `-no-color` Optionally turns off the fancy colorized output and disables ANSI redrawing.
 - `-help` Prints usage info and exits.

## Contribute

PRs accepted.

Small note: If editing the README, please conform to the [standard-readme](https://github.com/RichardLitt/standard-readme) specification.

## License
Apache 2.0
 © IBM jStart
