#define _GNU_SOURCE
#include <sys/types.h>
#include <sys/wait.h>
#include <sys/syscall.h>
#include <unistd.h>
#include <fcntl.h>
#include <pwd.h>
#include <grp.h>
#include <stdlib.h>
#include <errno.h>
#include <dlfcn.h>
#include <string.h>

/* The native kernel lacks fnOS TRIM ACL's private faccessat(-21586) ABI.
 * For normal native-volume access checks, perform real VFS permission checks
 * in an isolated child with the requested user's real UID/GID/groups.
 * This does not implement or claim Windows/TRIM ACL support.
 */
int trim_access(unsigned uid,const char *path,int mode,int atflags,unsigned flags){
 if(!path || strncmp(path,"/vol1/",6) || atflags || flags || (mode & ~7)){
  int(*original)(unsigned,const char*,int,int,unsigned)=dlsym(RTLD_NEXT,"trim_access");
  if(original)return original(uid,path,mode,atflags,flags);
  errno=EOPNOTSUPP;return -1;
 }
 if(geteuid()!=0){
  if(uid!=getuid()){errno=EPERM;return -1;}
  return syscall(SYS_faccessat,AT_FDCWD,path,mode);
 }
 char buffer[16384];struct passwd entry,*pw=NULL;
 int err=getpwuid_r(uid,&entry,buffer,sizeof(buffer),&pw);
 if(err||!pw){errno=err?err:EACCES;return -1;}
 int count=16;gid_t *groups=malloc(count*sizeof(gid_t));if(!groups){errno=ENOMEM;return -1;}
 if(getgrouplist(pw->pw_name,pw->pw_gid,groups,&count)<0){
  if(count<1||count>65536){free(groups);errno=EACCES;return -1;}
  gid_t *larger=realloc(groups,count*sizeof(gid_t));if(!larger){free(groups);errno=ENOMEM;return -1;}groups=larger;
  if(getgrouplist(pw->pw_name,pw->pw_gid,groups,&count)<0){free(groups);errno=EACCES;return -1;}
 }
 int fds[2];if(pipe2(fds,O_CLOEXEC)){free(groups);return -1;}
 pid_t child=fork();
 if(child<0){err=errno;close(fds[0]);close(fds[1]);free(groups);errno=err;return -1;}
 if(!child){
  int result=0;syscall(SYS_close,fds[0]);
  if(syscall(SYS_setgroups,count,groups)||syscall(SYS_setresgid,pw->pw_gid,pw->pw_gid,pw->pw_gid)||syscall(SYS_setresuid,uid,uid,uid))result=errno;
  else if(syscall(SYS_faccessat,AT_FDCWD,path,mode))result=errno;
  syscall(SYS_write,fds[1],&result,sizeof(result));
  syscall(SYS_exit,0);__builtin_unreachable();
 }
 close(fds[1]);int result=EIO,status=0;ssize_t got;
 do {got=read(fds[0],&result,sizeof(result));}while(got<0&&errno==EINTR);
 close(fds[0]);free(groups);
 pid_t waited;do{waited=waitpid(child,&status,0);}while(waited<0&&errno==EINTR);
 if(got!=sizeof(result)||waited!=child||!WIFEXITED(status)||WEXITSTATUS(status)){errno=EIO;return -1;}
 if(result){errno=result;return -1;}return 0;
}
