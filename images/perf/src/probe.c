/*
 * probe.c — tests the sandbox from the inside with raw syscalls, so results do not depend
 * on which shell tools labbase kept. Used by labd/perf/p0/p0.sh. Each subcommand prints
 * one line "probe <name> key=value ..." and exits 0 when it ran (the verdict is p0.sh's).
 *
 *   probe id        uid, gid, effective capabilities, no_new_privs
 *   probe net       TCP connect to 1.1.1.1:80 and UDP send; list interfaces
 *   probe rofs      create a file in / and in /etc
 *   probe noexec    copy /bin/busybox to /tmp, chmod +x, try to exec it
 *   probe fill      write to /tmp until an error; report bytes written
 *   probe fork      fork sleeping children until fork fails; report how many
 *   probe rlimits   print NOFILE, FSIZE, NPROC, CORE hard limits
 *   probe mem MB    allocate and touch MB megabytes, reporting progress (OOM kills us)
 *   probe cpu S     spin for S seconds of wall time; report CPU seconds used
 */
#define _GNU_SOURCE
#include <arpa/inet.h>
#include <errno.h>
#include <fcntl.h>
#include <ifaddrs.h>
#include <netinet/in.h>
#include <signal.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/prctl.h>
#include <sys/resource.h>
#include <sys/socket.h>
#include <sys/stat.h>
#include <sys/wait.h>
#include <time.h>
#include <unistd.h>

/* errno names for the outcomes P0 cares about (musl has no strerrorname_np). */
static const char *err(int e)
{
	switch (e) {
	case 0: return "OK";
	case EACCES: return "EACCES";
	case EPERM: return "EPERM";
	case EROFS: return "EROFS";
	case ENOSPC: return "ENOSPC";
	case EFBIG: return "EFBIG";
	case EAGAIN: return "EAGAIN";
	case ENOMEM: return "ENOMEM";
	case ENETUNREACH: return "ENETUNREACH";
	case EHOSTUNREACH: return "EHOSTUNREACH";
	case ECONNREFUSED: return "ECONNREFUSED";
	case EAFNOSUPPORT: return "EAFNOSUPPORT";
	case ENOENT: return "ENOENT";
	case ENOEXEC: return "ENOEXEC";
	case ETIMEDOUT: return "ETIMEDOUT";
	default: return "EOTHER";
	}
}

static int p_id(void)
{
	char line[256], cap[64] = "?";
	FILE *f = fopen("/proc/self/status", "r");
	while (f && fgets(line, sizeof line, f))
		if (strncmp(line, "CapEff:", 7) == 0)
			sscanf(line + 7, "%63s", cap);
	if (f)
		fclose(f);
	printf("probe id uid=%d gid=%d capeff=%s nnp=%d\n", getuid(), getgid(), cap,
	       prctl(PR_GET_NO_NEW_PRIVS, 0, 0, 0, 0));
	return 0;
}

static int p_net(void)
{
	int ifs = 0;
	struct ifaddrs *ia = NULL;
	if (getifaddrs(&ia) == 0)
		for (struct ifaddrs *p = ia; p; p = p->ifa_next)
			ifs++;
	freeifaddrs(ia);

	struct sockaddr_in a = {.sin_family = AF_INET, .sin_port = htons(80)};
	inet_pton(AF_INET, "1.1.1.1", &a.sin_addr);
	int s = socket(AF_INET, SOCK_STREAM, 0);
	int tcp_errno = 0;
	if (s < 0)
		tcp_errno = errno;
	else if (connect(s, (struct sockaddr *)&a, sizeof a) != 0)
		tcp_errno = errno;
	int u = socket(AF_INET, SOCK_DGRAM, 0), udp_errno = 0;
	if (u < 0 || sendto(u, "x", 1, 0, (struct sockaddr *)&a, sizeof a) < 0)
		udp_errno = errno;
	printf("probe net interfaces=%d tcp=%s udp=%s\n", ifs, err(tcp_errno), err(udp_errno));
	return 0;
}

static int p_rofs(void)
{
	int a = open("/p0-root-test", O_CREAT | O_WRONLY, 0644), ea = a < 0 ? errno : 0;
	int b = open("/etc/p0-etc-test", O_CREAT | O_WRONLY, 0644), eb = b < 0 ? errno : 0;
	printf("probe rofs root=%s etc=%s\n", err(ea), err(eb));
	return 0;
}

static int p_noexec(void)
{
	int in = open("/bin/busybox", O_RDONLY), out = open("/tmp/p0-bb", O_CREAT | O_WRONLY | O_TRUNC, 0755);
	char buf[65536];
	ssize_t n;
	while (in >= 0 && out >= 0 && (n = read(in, buf, sizeof buf)) > 0)
		if (write(out, buf, (size_t)n) != n)
			break;
	close(in);
	close(out);
	chmod("/tmp/p0-bb", 0755);
	char *const argv[] = {"true", NULL};
	execv("/tmp/p0-bb", argv); /* returns only on failure */
	printf("probe noexec exec=%s\n", err(errno));
	return 0;
}

static int p_fill(void)
{
	int fd = open("/tmp/p0-fill", O_CREAT | O_WRONLY | O_TRUNC, 0600);
	static char buf[1 << 20];
	long long total = 0;
	int e = 0;
	signal(SIGXFSZ, SIG_IGN); /* see EFBIG instead of dying if RLIMIT_FSIZE hits first */
	for (int i = 0; i < 64; i++) {
		ssize_t n = write(fd, buf, sizeof buf);
		if (n < 0) {
			e = errno;
			break;
		}
		total += n;
		if (n < (ssize_t)sizeof buf) {
			e = ENOSPC;
			break;
		}
	}
	close(fd);
	unlink("/tmp/p0-fill");
	printf("probe fill bytes=%lld error=%s\n", total, err(e));
	return 0;
}

static int p_fork(void)
{
	int n = 0, e = 0;
	pid_t kids[256];
	for (; n < 256; n++) {
		pid_t p = fork();
		if (p < 0) {
			e = errno;
			break;
		}
		if (p == 0) {
			pause();
			_exit(0);
		}
		kids[n] = p;
	}
	for (int i = 0; i < n; i++)
		kill(kids[i], SIGKILL);
	while (wait(NULL) > 0)
		;
	printf("probe fork forked=%d error=%s\n", n, err(e));
	return 0;
}

static int p_rlimits(void)
{
	struct rlimit r;
	int res[] = {RLIMIT_NOFILE, RLIMIT_FSIZE, RLIMIT_NPROC, RLIMIT_CORE};
	const char *names[] = {"nofile", "fsize", "nproc", "core"};
	printf("probe rlimits");
	for (int i = 0; i < 4; i++) {
		getrlimit(res[i], &r);
		if (r.rlim_max == RLIM_INFINITY)
			printf(" %s=inf", names[i]);
		else
			printf(" %s=%llu", names[i], (unsigned long long)r.rlim_max);
	}
	printf("\n");
	return 0;
}

static int p_mem(int mb)
{
	setvbuf(stdout, NULL, _IONBF, 0);
	for (int i = 1; i <= mb; i++) {
		char *p = malloc(1 << 20);
		if (!p) {
			printf("probe mem allocated_mb=%d error=ENOMEM\n", i - 1);
			return 0;
		}
		memset(p, i, 1 << 20);
		if (i % 16 == 0)
			printf("probe mem progress_mb=%d\n", i);
	}
	printf("probe mem allocated_mb=%d error=OK\n", mb);
	return 0;
}

static double now(clockid_t c)
{
	struct timespec t;
	clock_gettime(c, &t);
	return (double)t.tv_sec + (double)t.tv_nsec / 1e9;
}

static int p_cpu(int secs)
{
	double w0 = now(CLOCK_MONOTONIC), c0 = now(CLOCK_PROCESS_CPUTIME_ID);
	volatile unsigned long x = 0;
	while (now(CLOCK_MONOTONIC) - w0 < secs)
		x++;
	double wall = now(CLOCK_MONOTONIC) - w0, cpu = now(CLOCK_PROCESS_CPUTIME_ID) - c0;
	printf("probe cpu wall_s=%.2f cpu_s=%.2f ratio=%.2f\n", wall, cpu, cpu / wall);
	return 0;
}

int main(int argc, char **argv)
{
	setvbuf(stdout, NULL, _IOLBF, 0);
	const char *c = argc > 1 ? argv[1] : "";
	if (!strcmp(c, "id")) return p_id();
	if (!strcmp(c, "net")) return p_net();
	if (!strcmp(c, "rofs")) return p_rofs();
	if (!strcmp(c, "noexec")) return p_noexec();
	if (!strcmp(c, "fill")) return p_fill();
	if (!strcmp(c, "fork")) return p_fork();
	if (!strcmp(c, "rlimits")) return p_rlimits();
	if (!strcmp(c, "mem")) return p_mem(argc > 2 ? atoi(argv[2]) : 200);
	if (!strcmp(c, "cpu")) return p_cpu(argc > 2 ? atoi(argv[2]) : 3);
	fprintf(stderr, "usage: probe id|net|rofs|noexec|fill|fork|rlimits|mem MB|cpu S\n");
	return 2;
}
