#define _GNU_SOURCE
#include <liburing.h>
#include <pthread.h>
#include <dlfcn.h>
#include <unistd.h>
#include <stdlib.h>
#include <string.h>
#include <stdio.h>
#include <limits.h>

/* Scoped fallback for the audited uploadsrv WRITE/CLOSE/NOP paths.
 * Real POSIX file operations generate actual completions, including errors.
 * This is not a replacement for io_uring in other applications.
 */
struct fallback {
 struct io_uring *ring;
 unsigned sq[8],cq[8];
 pthread_mutex_t lock;
 pthread_cond_t ready;
 struct fallback *next;
};
static pthread_mutex_t registry_lock=PTHREAD_MUTEX_INITIALIZER;
static struct fallback *registry;
static struct fallback *lookup(struct io_uring *ring) {
 struct fallback *f;
 pthread_mutex_lock(&registry_lock);
 for(f=registry;f && f->ring!=ring;f=f->next){}
 pthread_mutex_unlock(&registry_lock);
 return f;
}
static unsigned power2(unsigned n){unsigned p=1;while(p<n)p<<=1;return p;}
int io_uring_queue_init(unsigned entries,struct io_uring *ring,unsigned flags){
 if(!getenv("SP1099X_UPLOAD_POSIX_IO")) {
  int(*original)(unsigned,struct io_uring*,unsigned)=dlsym(RTLD_NEXT,"io_uring_queue_init");
  return original?original(entries,ring,flags):-ENOSYS;
 }
 if(!entries || entries>65536 || flags)return -EINVAL;
 struct fallback *f=calloc(1,sizeof(*f));if(!f)return -ENOMEM;
 unsigned n=power2(entries),nc=power2(n<2048?8192:n*4);
 memset(ring,0,sizeof(*ring)); f->ring=ring;
 ring->sq.sqes=calloc(n,sizeof(struct io_uring_sqe));
 ring->sq.array=calloc(n,sizeof(unsigned));
 ring->cq.cqes=calloc(nc,sizeof(struct io_uring_cqe));
 if(!ring->sq.sqes||!ring->sq.array||!ring->cq.cqes){free(ring->sq.sqes);free(ring->sq.array);free(ring->cq.cqes);free(f);return -ENOMEM;}
 f->sq[2]=n-1;f->sq[3]=n;f->cq[2]=nc-1;f->cq[3]=nc;
 ring->sq.khead=&f->sq[0];ring->sq.ktail=&f->sq[1];ring->sq.kring_mask=&f->sq[2];ring->sq.kring_entries=&f->sq[3];ring->sq.kflags=&f->sq[4];ring->sq.kdropped=&f->sq[5];
 ring->cq.khead=&f->cq[0];ring->cq.ktail=&f->cq[1];ring->cq.kring_mask=&f->cq[2];ring->cq.kring_entries=&f->cq[3];ring->cq.kflags=&f->cq[4];ring->cq.koverflow=&f->cq[5];
 ring->sq.ring_mask=n-1;ring->sq.ring_entries=n;ring->cq.ring_mask=nc-1;ring->cq.ring_entries=nc;ring->ring_fd=-1;ring->enter_ring_fd=-1;
 pthread_mutex_init(&f->lock,NULL);pthread_cond_init(&f->ready,NULL);
 pthread_mutex_lock(&registry_lock);f->next=registry;registry=f;pthread_mutex_unlock(&registry_lock);
 fprintf(stderr,"SP1099X upload POSIX I/O fallback active (real file operations)\n");
 return 0;
}
static int execute(const struct io_uring_sqe *s){
 ssize_t result;
 if(s->flags & ~IOSQE_IO_DRAIN)return -EOPNOTSUPP;
 if(s->len>INT_MAX)return -EINVAL;
 switch(s->opcode){
 case IORING_OP_NOP:return 0;
 case IORING_OP_WRITE:
  if(s->rw_flags)return -EOPNOTSUPP;
  do {result=s->off==(__u64)-1?write(s->fd,(void*)(uintptr_t)s->addr,s->len):pwrite(s->fd,(void*)(uintptr_t)s->addr,s->len,s->off);} while(result<0&&errno==EINTR);
  break;
 case IORING_OP_READ:
  if(s->rw_flags)return -EOPNOTSUPP;
  do {result=s->off==(__u64)-1?read(s->fd,(void*)(uintptr_t)s->addr,s->len):pread(s->fd,(void*)(uintptr_t)s->addr,s->len,s->off);} while(result<0&&errno==EINTR);
  break;
 case IORING_OP_WRITEV:
  if(s->rw_flags || s->len>IOV_MAX)return -EOPNOTSUPP;
  do {result=s->off==(__u64)-1?writev(s->fd,(void*)(uintptr_t)s->addr,s->len):pwritev(s->fd,(void*)(uintptr_t)s->addr,s->len,s->off);} while(result<0&&errno==EINTR);
  break;
 case IORING_OP_FSYNC:
  if(s->fsync_flags & ~IORING_FSYNC_DATASYNC)return -EOPNOTSUPP;
  do {result=(s->fsync_flags & IORING_FSYNC_DATASYNC)?fdatasync(s->fd):fsync(s->fd);}while(result<0&&errno==EINTR);
  break;
 case IORING_OP_CLOSE:result=close(s->fd);break;
 default:fprintf(stderr,"Unsupported upload I/O opcode %u\n",s->opcode);return -EOPNOTSUPP;
 }
 return result<0?-errno:(int)result;
}
int io_uring_submit(struct io_uring *ring){
 struct fallback *f=lookup(ring);
 if(!f){int(*original)(struct io_uring*)=dlsym(RTLD_NEXT,"io_uring_submit");return original?original(ring):-EINVAL;}
 pthread_mutex_lock(&f->lock);unsigned count=0;
 while(ring->sq.sqe_head!=ring->sq.sqe_tail){
  unsigned tail=__atomic_load_n(ring->cq.ktail,__ATOMIC_RELAXED),head=__atomic_load_n(ring->cq.khead,__ATOMIC_ACQUIRE);
  if(tail-head>=ring->cq.ring_entries){pthread_mutex_unlock(&f->lock);return count?(int)count:-EAGAIN;}
  struct io_uring_sqe *s=&ring->sq.sqes[ring->sq.sqe_head & ring->sq.ring_mask];
  struct io_uring_cqe *c=&ring->cq.cqes[tail & ring->cq.ring_mask];
  c->user_data=s->user_data;c->res=execute(s);c->flags=0;
  ring->sq.sqe_head++;count++;
  __atomic_store_n(ring->sq.khead,ring->sq.sqe_head,__ATOMIC_RELEASE);
  __atomic_store_n(ring->sq.ktail,ring->sq.sqe_head,__ATOMIC_RELEASE);
  __atomic_store_n(ring->cq.ktail,tail+1,__ATOMIC_RELEASE);
 }
 pthread_cond_broadcast(&f->ready);pthread_mutex_unlock(&f->lock);return count;
}
int __io_uring_get_cqe(struct io_uring *ring,struct io_uring_cqe **out,unsigned submit,unsigned wait_nr,sigset_t *mask){
 struct fallback *f=lookup(ring);
 if(!f){int(*original)(struct io_uring*,struct io_uring_cqe**,unsigned,unsigned,sigset_t*)=dlsym(RTLD_NEXT,"__io_uring_get_cqe");return original?original(ring,out,submit,wait_nr,mask):-EINVAL;}
 if(mask)return -EOPNOTSUPP;
 if(submit){int ret=io_uring_submit(ring);if(ret<0)return ret;}
 pthread_mutex_lock(&f->lock);
 while(__atomic_load_n(ring->cq.khead,__ATOMIC_ACQUIRE)==__atomic_load_n(ring->cq.ktail,__ATOMIC_ACQUIRE)){
  if(!wait_nr){*out=NULL;pthread_mutex_unlock(&f->lock);return -EAGAIN;}
  pthread_cond_wait(&f->ready,&f->lock);
 }
 unsigned head=__atomic_load_n(ring->cq.khead,__ATOMIC_RELAXED);
 *out=&ring->cq.cqes[head & ring->cq.ring_mask];
 pthread_mutex_unlock(&f->lock);return 0;
}
void io_uring_queue_exit(struct io_uring *ring){
 struct fallback *f=lookup(ring);
 if(!f){void(*original)(struct io_uring*)=dlsym(RTLD_NEXT,"io_uring_queue_exit");if(original)original(ring);return;}
 /* The audited service exits as a process; queue_exit requires no active users. */
 pthread_mutex_lock(&registry_lock);struct fallback **p=&registry;while(*p&&*p!=f)p=&(*p)->next;if(*p)*p=f->next;pthread_mutex_unlock(&registry_lock);
 free(ring->sq.sqes);free(ring->sq.array);free(ring->cq.cqes);
 pthread_cond_destroy(&f->ready);pthread_mutex_destroy(&f->lock);free(f);memset(ring,0,sizeof(*ring));
}
