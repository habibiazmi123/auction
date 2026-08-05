#!/usr/bin/env bash
set -euo pipefail

brokers="${KAFKA_BROKERS:-kafka:9092}"
partitions="${KAFKA_TOPIC_PARTITIONS:-6}"
replication_factor="${KAFKA_TOPIC_REPLICATION_FACTOR:-1}"
kafka_topics="${KAFKA_TOPICS_BIN:-/opt/bitnami/kafka/bin/kafka-topics.sh}"

for topic in \
  auction.bid.commands.v1 \
  auction.events.v1 \
  product.events.v1 \
  user.events.v1 \
  transaction.events.v1 \
  notification.events.v1; do
  "$kafka_topics" \
    --bootstrap-server "$brokers" \
    --create \
    --if-not-exists \
    --topic "$topic" \
    --partitions "$partitions" \
    --replication-factor "$replication_factor"
done
