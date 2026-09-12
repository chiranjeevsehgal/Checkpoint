import json
import uuid
from datetime import datetime, timezone

from confluent_kafka import Consumer, Producer, TopicPartition


class Kafka:
    """Offsets commit only after a message is fully processed (embedded + stored);
    a crash mid-processing means redelivery, mirroring the transcription
    service's semantics. Poison messages go to `<topic>.dlq` (pre-created by
    kafka-init) with the same envelope shape as every other event on the bus.
    """

    def __init__(self, brokers: str, topic: str, consumer_group: str) -> None:
        self._topic = topic
        self._consumer = Consumer(
            {
                "bootstrap.servers": brokers,
                "group.id": consumer_group,
                "enable.auto.commit": False,
                "auto.offset.reset": "earliest",
            }
        )
        self._consumer.subscribe([topic])
        self._producer = Producer({"bootstrap.servers": brokers})

    @property
    def topic(self) -> str:
        return self._topic

    @property
    def dlq_topic(self) -> str:
        return f"{self._topic}.dlq"

    def poll(self, timeout: float):
        return self._consumer.poll(timeout)

    def commit(self, message) -> None:
        self._consumer.commit(message=message, asynchronous=False)

    def seek(self, partition: int, offset: int) -> None:
        self._consumer.seek(TopicPartition(self._topic, partition, offset))

    def send_to_dlq(self, error_code: str, error_message: str, original_payload: bytes) -> None:
        try:
            original = json.loads(original_payload)
        except (json.JSONDecodeError, UnicodeDecodeError):
            original = original_payload.decode("utf-8", errors="replace")
        event = {
            "schema_version": 2,
            "event_id": str(uuid.uuid4()),
            "event_type": "EMBEDDING_FAILED",
            "occurred_at": datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%S.%f")[:-3] + "Z",
            "data": {
                "source_topic": self._topic,
                "error_code": error_code,
                "error_message": error_message,
                "original_event": original,
            },
        }
        self._producer.produce(
            self.dlq_topic,
            key=None,
            value=json.dumps(event).encode("utf-8"),
        )
        self._producer.flush(10.0)

    @staticmethod
    def error(message) -> bool:
        return message is not None and message.error() is not None

    def close(self) -> None:
        self._consumer.close()
        self._producer.flush(10.0)
