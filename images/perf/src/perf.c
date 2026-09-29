/*
 * perf.c — the program behind P0 (gdb feature checks under gVisor) and the perf suite.
 * Not a challenge: it has one of each thing gdb needs to handle, and markers the P0
 * script finds by grepping this file (lines containing "P0-").
 *
 * Build: gcc -O0 -g -no-pie -fno-pie -fno-stack-protector -pthread (images/perf/build.sh)
 * Usage: perf            normal run; prints main address, totals, report line
 *        perf addr       print addresses and exit (ASLR check without gdb)
 *        perf crash      NULL dereference (used to generate perf.core)
 */
#include <pthread.h>
#include <signal.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

struct account {
	char name[8];
	unsigned checksum;
	int balance;
};

static struct account accounts[4] = {
	{"alice", 0, 100}, {"bob", 0, 250}, {"carol", 0, 75}, {"dave", 0, 40},
};
static int scores[5] = {10, 20, 30, 40, 50};
static int after_scores = 7; /* what the off-by-one reads, on most layouts */

volatile int counter;              /* watchpoint target */
volatile sig_atomic_t got_usr1;    /* set by the SIGUSR1 handler */
static pthread_mutex_t mu = PTHREAD_MUTEX_INITIALIZER;
static pthread_mutex_t gate = PTHREAD_MUTEX_INITIALIZER;

/* Off-by-one: i <= n reads one element past the end. */
int sum_scores(int n)
{
	int total = 0;
	for (int i = 0; i <= n; i++)
		total += scores[i];
	return total;
}

struct account *find(const char *name)
{
	for (int i = 0; i < 4; i++)
		if (strcmp(accounts[i].name, name) == 0)
			return &accounts[i];
	return NULL;
}

unsigned checksum(const struct account *a)
{
	unsigned h = 2166136261u;
	for (size_t i = 0; i < sizeof a->name; i++)
		h = (h ^ (unsigned char)a->name[i]) * 16777619u;
	return h ^ (unsigned)a->balance;
}

void *worker(void *arg)
{
	(void)arg;
	pthread_mutex_lock(&gate); /* held by main until threads_ready() returns */
	pthread_mutex_unlock(&gate);
	for (int i = 0; i < 1000; i++) {
		pthread_mutex_lock(&mu);
		counter++;
		pthread_mutex_unlock(&mu);
	}
	return NULL;
}

/* Breakpoint target: all three workers exist and are blocked on `gate`. */
void threads_ready(void) {}

void on_usr1(int sig)
{
	(void)sig;
	got_usr1 = 1;
}

/* A short loop that writes `counter` often: the watchpoint target. */
int step_loop(int n)
{
	int acc = 0;
	for (int i = 0; i < n; i++) {
		acc = acc * 31 + i;
		counter = acc & 0xff;
	}
	return acc;
}

void crash(void)
{
	struct account *a = find("nobody");
	printf("%d\n", a->balance); /* SIGSEGV: a is NULL */
}

void report(unsigned key)
{
	printf("report key=%u\n", key);
}

int main(int argc, char **argv)
{
	int local = 0;
	void *heap = malloc(64);

	if (argc > 1 && strcmp(argv[1], "addr") == 0) {
		printf("addr main=%p stack=%p heap=%p\n", (void *)main, (void *)&local, heap);
		return 0;
	}
	if (argc > 1 && strcmp(argv[1], "crash") == 0)
		crash();

	signal(SIGUSR1, on_usr1);
	printf("main=%p\n", (void *)main);

	int total = sum_scores(4);
	printf("total=%d\n", total);

	for (int i = 0; i < 4; i++)
		accounts[i].checksum = checksum(&accounts[i]);
	struct account *b = find("bob");
	printf("bob=%d checksum=%u\n", b ? b->balance : -1, b ? b->checksum : 0);

	pthread_t t[3];
	pthread_mutex_lock(&gate);
	for (int i = 0; i < 3; i++)
		pthread_create(&t[i], NULL, worker, NULL);
	threads_ready();
	pthread_mutex_unlock(&gate);
	for (int i = 0; i < 3; i++)
		pthread_join(t[i], NULL);
	printf("counter=%d\n", counter);

	int acc = step_loop(20);
	printf("acc=%d\n", acc);

	int flag = 0;
	flag = 1;                               /* P0-JUMP-FROM */
	printf("jumped=%d\n", flag == 0);      /* P0-JUMP-TO */

	printf("usr1=%d\n", (int)got_usr1);
	report((unsigned)total);
	free(heap);
	(void)after_scores;
	(void)local;
	return 0;
}
