//go:build staticpie

// The entry point of the static-PIE Linux build (make dist links it with
// -e gecko_start), run before musl's own _start. See staticpie.go.
//
// Started by the kernel, it changes nothing. Started by Android's linker
// (/system/bin/linker64 /path/to/gecko args), three things differ from a
// normal start, and it puts them back the way musl expects:
//   - argv[0] is the linker and argv[1] gecko: argv moves up by one;
//   - the aux vector describes the linker (AT_PHDR and the rest): it is
//     pointed at gecko's own program headers, where musl finds its base
//     address and the TLS segment;
//   - the linker has already relocated gecko and made its RELRO pages
//     read-only, and musl relocates again (the same values): those pages
//     are made writable again, as they are after a kernel start.
//
// Nothing here may need a relocation, a thread pointer or the stack
// protector: it runs before any of them exist.

#include <elf.h>
#include <link.h>
#include <stddef.h>

extern const ElfW(Ehdr) __ehdr_start __attribute__((visibility("hidden")));

// bionic's linker refuses an arm64 executable whose TLS segment is aligned
// to less than 64 bytes; this raises it (Go's and musl's TLS need only 8).
// gecko_start refers to it so that the linker keeps it.
__thread char gecko_tls_align[64] __attribute__((aligned(64), used, visibility("hidden")));

static inline long raw_mprotect(size_t addr, size_t len, long prot)
{
#if defined(__x86_64__)
	long r;
	__asm__ volatile("syscall" : "=a"(r) : "0"(10L), "D"(addr), "S"(len), "d"(prot) : "rcx", "r11", "memory");
	return r;
#elif defined(__aarch64__)
	register long x8 __asm__("x8") = 226;
	register long x0 __asm__("x0") = (long)addr;
	register long x1 __asm__("x1") = (long)len;
	register long x2 __asm__("x2") = prot;
	__asm__ volatile("svc 0" : "+r"(x0) : "r"(x8), "r"(x1), "r"(x2) : "memory");
	return x0;
#else
#error "staticpie: amd64 and arm64 only"
#endif
}

// gecko_fix_start takes the initial stack pointer (argc, argv, envp, auxv)
// and returns the one to start musl with.
__attribute__((visibility("hidden"), used, no_stack_protector))
size_t *gecko_fix_start(size_t *sp)
{
	size_t argc = sp[0];
	char **p = (char **)(sp + 1) + argc + 1;
	while (*p)
		p++;
	size_t base = (size_t)&__ehdr_start;
	size_t own = base + __ehdr_start.e_phoff;
	size_t *phdr = 0, *phnum = 0, *entry = 0, *ldbase = 0;
	size_t pagesz = 4096;
	for (size_t *a = (size_t *)(p + 1); a[0] != AT_NULL; a += 2) {
		if (a[0] == AT_PHDR)
			phdr = a;
		else if (a[0] == AT_PHNUM)
			phnum = a;
		else if (a[0] == AT_ENTRY)
			entry = a;
		else if (a[0] == AT_BASE)
			ldbase = a;
		else if (a[0] == AT_PAGESZ && a[1])
			pagesz = a[1];
	}
	if (!phdr || phdr[1] == own || argc < 2)
		return sp; // started by the kernel
	phdr[1] = own;
	if (phnum)
		phnum[1] = __ehdr_start.e_phnum;
	if (entry)
		entry[1] = base + __ehdr_start.e_entry;
	if (ldbase)
		ldbase[1] = 0;
	const ElfW(Phdr) *ph = (const ElfW(Phdr) *)own;
	for (size_t i = 0; i < __ehdr_start.e_phnum; i++) {
		if (ph[i].p_type != PT_GNU_RELRO)
			continue;
		size_t start = (base + ph[i].p_vaddr) & ~(pagesz - 1);
		size_t end = base + ph[i].p_vaddr + ph[i].p_memsz;
		raw_mprotect(start, end - start, 3); // PROT_READ|PROT_WRITE
	}
	sp[1] = argc - 1; // over argv[0], the linker
	return sp + 1;
}

// musl's _start (rcrt1.o) takes the stack pointer as the kernel left it;
// on arm64 the moved one is 8 off 16-byte alignment, which _start fixes
// before it touches the stack.
#if defined(__x86_64__)
__asm__(
	".text\n"
	".global gecko_start\n"
	".hidden gecko_start\n"
	".type gecko_start,@function\n"
	"gecko_start:\n"
	"	mov %rsp, %rdi\n"
	"	and $-16, %rsp\n"
	"	call gecko_fix_start\n"
	"	mov %rax, %rsp\n"
	"	xor %edx, %edx\n"
	"	jmp _start\n"
	"	mov %fs:gecko_tls_align@tpoff, %al\n"); // never run: keeps gecko_tls_align
#elif defined(__aarch64__)
__asm__(
	".text\n"
	".global gecko_start\n"
	".hidden gecko_start\n"
	".type gecko_start,%function\n"
	"gecko_start:\n"
	"	mov x0, sp\n"
	"	bl gecko_fix_start\n"
	"	mov sp, x0\n"
	"	mov x30, #0\n"
	"	b _start\n"
	"	add x0, x0, #:tprel_lo12_nc:gecko_tls_align\n"); // never run: keeps gecko_tls_align
#endif
