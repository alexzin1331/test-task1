## Description

The application is designed to track the prices of cryptocurrencies.
It has 3 POST-handlers:
- add (adding cryptocurrencies to tracking)
- remove (removing cryptocurrencies from tracking)
- price (receiving the price at the specified time)

If the time point is not specified, the current time is automatically inserted.

Launch Instructions:
1) git clone https://github.com/alexzin1331/test-task1.git
2) cd test-task1
3) docker-compose up --build

After the launch, the documentation can be found at [link](http://localhost:8080/swagger/index.html )

## Implementation Details:
- Implemented caching to speed up data acquisition
  It works as follows:
  1) 100 token keys are stored in the cache, for each of which price data for the last 4 hours is stored (price + unix time)
  2) The key update logic works in accordance with LRU (LRU limits: memory - 100mb, time - 10 minutes, number of records - 100; the parameters are set to constants and can be changed if necessary), and the data within a single token is updated every time it is read (records older than 4 hours are deleted).
  3) Testing of such memory optimization showed the following results (taking one token):
     - Get from cache, time (ns): 825375 (0.8 ms)
     - Get from PostgreSQL, time (ns): 23537166 (23 ms)
  4) Within each token, a redis set is implemented for accelerated sampling of the nearest date from cache
- Storage is covered by tests
- An index has been created for accelerated sampling from PostgreSQL: CREATE INDEX idx_currencies_coin_timestamp ON currencies (coin, timestamp);
- The implementation of the receipt turned out to be quite difficult due to the peculiarities of the names of cryptocurrencies in the kraken api (data is parsed through the API and a map is created that matches the name of the familiar token name and the name in the API) (the whole code consists of unmarshal and typecasting.)

