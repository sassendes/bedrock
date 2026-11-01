---
title: "Rotate the proof key"
weight: 13
description: "Replace the key postgres signs proofs of life with, without the failover controller losing a round."
---

Postgres signs every proof of life with the active key in `bedrock_proof_key`. The verifier checks each signature against the keys in the `bedrock-proof-keys` Secret. The keys are kept on the controller machine in `~/.bedrock/failover/proof-keys/`, one file per key, named by the time it was made, such as `k20261101180000`.

A rotation adds the new key everywhere before postgres uses it, so no signature is ever made with a key the verifier does not know.

## Run it

From the repository root, on the controller machine:

```sh
make rotate-proof-key
```

The playbook is `tenant-workload/bootstrap/rotate-proof-key.yml`. It runs these steps:

| Step | Effect |
|---|---|
| 1 | Generates a new 32-byte key in `~/.bedrock/failover/proof-keys/` |
| 2 | Inserts it into `bedrock_proof_key` on the cluster-1 primary, inactive |
| 3 | Rebuilds `bedrock-proof-keys` with the old and the new key and restarts the verifier |
| 4 | Makes the new key the only active key, so postgres signs with it from the next round |
| 5 | Waits 60 seconds, so every round signed with the old key has closed |
| 6 | Deletes the old keys from postgres and from the controller machine |
| 7 | Rebuilds `bedrock-proof-keys` with the new key only and restarts the verifier |

The key never appears on a command line. It goes to postgres through standard input and to the Secret from the file.

## Check it

```sh
kubectl -n bedrock-failover logs deploy/bedrock-verifier | grep 'verifier started'
kubectl -n bedrock-failover get configmap bedrock-round -o jsonpath='{.data.round}'
```

The verifier logs how many keys it loaded when it starts. After the rotation it is 1. The round's `validUps` still lists observers for every primary, which means the new signatures verify.

## If it stops half way

Every step can be run again. If the playbook fails after step 3, the verifier trusts both keys, so nothing breaks. Run `make rotate-proof-key` again, or finish by hand: make sure only one key file is left in `~/.bedrock/failover/proof-keys/`, that the same key is the only row in `bedrock_proof_key`, and rebuild the Secret from that directory.
