# Polymer x Celestia Hackathon
Given L2 block number and certain L2 blockchain paramters the application scans sepolia's batcher inbox in Sepolia
for transactions that have Celestia blob id and prints out what it finds in Celestia after fetching the blob by id.

## Installation
The application requires a running DA client locally that is synced to the blob you are searching for.
```
celestia light start --core.ip consensus.lunaroasis.net
```

Existing problems:
- The application finds data in Celestia that has an Optimism frame. The frame has a lot of bytes in it but the decoding of it into a batch always 
returns no transactions in the batch. 

Log example:
```
L2 Block Number: 12717114
L2 Block Hash: 0x7b278d0e517a20821fd4c59cbb670b5d5ce405d5b5eede1b758feae26ca813e3
L2 Block Time: 1714540955
Number of L2 Transactions:  1
Looking in the next blocks on Ethereum (L1) after block 19773157 to find batches posted to batch inbox address 
   8% |███████████████                   | (99/1200, 5 it/s) [46s:4m3s]
Celestia: found a celestia starting byte in calldate id cea48e140000000000bb2e546c0795c7c805abcd34ca4f7ceaee556ec23748e3a542db8b13f73d43e8
frames length: 1
frame data length: 119976
txs length in the batch 0
```