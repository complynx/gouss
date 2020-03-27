## gouss -- Go Url Simple Shortener

Model program to demonstrate programming skills. (time 3h 53m, including DB selection)

This URL shortener provides a very basic URL shortening service: no logins, no URL or XSS tests.
The solution is very primitive and robust, the only requirements in mind are the requirements
described in the task:

* Short Url
    1. Has one long url
    2. Permanent; Once created
    3. Is Unique; If a long url is added twice it should result in two different short urls
    4. Not easily discoverable; incrementing an already existing short url should
    have a low probability of finding a working short url.
* Other specs 
    1. Generating a short url from a long url
    2. Redirecting a short url to a long url within 10 ms.
    3. Listing the number of times a short url has been accessed in the last 24 hours,
        past week and all time.
    4. Persistence (data must survive computer restarts)

## Build

1. Install dependencies from `go.mod`
2. build: `$ go build -i -o gouss`

## Run

`$ ./gouss`

## Test

Service binds to **8077** port, so you can use CURL or any other software to test.

Usage:
* `GET http://localhost:8077/` -- Prints usage
* `POST http://localhost:8077/set` -- Gets URL in the body as plain text,
    returns the shortened one as a plain text
* `GET http://localhost:8077/<StndURL>` -- Redirects to a saved URL
* `GET http://localhost:8077/<StndURL>/stat` -- Prints statistics about the URL

No tests included because it'd taken a lot of additional time. You can run tests in
Postman or other SW to test this service.

## Assumptions and design decisions

* As it is a model program, it lacks many features otherwise mandatory for this kind of
  software (all things stated here, I know how to solve, time provided)
  * No logic separation. For the sake of speed, everything is in one file.
  * No unit tests. Unit tests take a lot of time to develop.
  * Non-mockable design. Every entity is hard-bound into the design.
  * No http.Handler level.
  * Global variables.
  * No Panic catchers, three places with omitted errors.
  * No input test -- the program assumes the URL is correctly formatted (this one,
    I could go with another lib or two, but didn't want wasting time on the research).
  * Simplest time arithmetic.
  * No scalability.
  * No time-series optimizations.
  * Ideally, it has to be two services: URL shortener and hits calculator, or at least
    hits should be calculated in a separate DB like Prometheus
* Design decisions regarding the task
  * Language selection
    * Though Python would have been really fast to implement, it wouldn't provide you with
      information regardless my knowledge of Go.
  * Database selection
    * To store the data, the BadgerDB was chosen
      * It is a persistent k-v DB easily incorporatable in Go project without any need of 
        containerization and additional setup
      * While it stores everything on disk, the keys are always present in memory, so it is fast.
      * it has good fetch and update times.
    * I'd selected Prometheus or something similar to store time series data, but that would
      have been a lot more time consuming to develop.
      Therefore instead I use the following concept:
      * For hourly and weekly series I use a set of timestamps. At each hit, a current timestamp
        is added and the series is cleaned of old ones. If we'll assume no URL is hit millions
        of times during the week, it's OK. And there's no such requirement.
      * Overall hits are implemented as a simple counter.
  * The design has all the improvements listed above in mind, it is developed to be easily
    refactored to support all the stated features.
  * The redirect is sent prior to time-series logging to speed-up response times and reduce
    impact on critical flow.
  * ID selection for the shortener is very simple and designed to expand as the time progress
    Upon creating, it will randomly try to create a link and if in 50 times it didn't find a free
    address, it increases the size of the shortener by one symbol (and will use the new size
    from now on). If with the new size it didn't get good results, which would be rather strange,
    it will report an error. This will ensure that it is hard to bruteforce the shortened urls,
    it will increase size faster than it uses up all the space in the lower shortcuts.
