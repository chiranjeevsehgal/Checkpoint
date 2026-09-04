# Setup

## 1. Create `.env`

Create `.env` from `env.example`.

### Windows

```bash
copy env.example .env
```

### Linux / macOS

```bash
cp env.example .env
```

Update `.env` with your actual values..

---

## 2. Start everything

```bash
docker-compose up
```

Or run in detached mode:

```bash
docker-compose up -d
```

---

## 3. Check containers

```bash
docker-compose ps
```

Both `postgres` and `kafka` should be up and healthy.

---

## 4. Test PostgreSQL

```bash
docker-compose exec postgres psql -U <POSTGRES_USER> -d <POSTGRES_DB> -c "SELECT version();"
```

---

## 5. Test Kafka

### Create topic

```bash
docker-compose exec kafka /opt/kafka/bin/kafka-topics.sh --create --topic test-topic --bootstrap-server kafka:9092 --partitions 1 --replication-factor 1
```

### List topics

```bash
docker-compose exec kafka /opt/kafka/bin/kafka-topics.sh --list --bootstrap-server kafka:9092
```

### Produce messages

```bash
docker-compose exec kafka /opt/kafka/bin/kafka-console-producer.sh --topic test-topic --bootstrap-server kafka:9092
```

Enter messages:

```text
message 1
message 2
```

Press `Ctrl+C` to exit.

### Consume messages

```bash
docker-compose exec kafka /opt/kafka/bin/kafka-console-consumer.sh --topic test-topic --bootstrap-server kafka:9092
```

Press `Ctrl+C` to exit.

---

## 6. Stop containers

Stop and remove containers:

```bash
docker-compose down
```