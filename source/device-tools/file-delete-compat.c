#define _GNU_SOURCE
#include <sys/types.h>
#include <sys/stat.h>
#include <sys/syscall.h>
#include <sys/xattr.h>
#include <fcntl.h>
#include <unistd.h>
#include <errno.h>
#include <stdio.h>
#include <string.h>
#include <stdlib.h>
#include <time.h>
#include <limits.h>

/* Scoped fnOS private unlinkat ABI fallback. The filemanager has already
 * dropped uid/gid/groups. All operations retain those VFS credentials.
 * Trash layout and record format match the installed trashbind binary.
 * No symlink traversal, cross-user operation, or trash-to-permanent fallback.
 */
static int parent_fd(int base, const char *relative, int create, char *name) {
 char path[PATH_MAX];
 if(strlen(relative)>=sizeof(path)){errno=ENAMETOOLONG;return -1;}
 strcpy(path,relative);int fd=dup(base);if(fd<0)return -1;
 char *part=path;unsigned depth=0;
 for(;;){
  char *slash=strchr(part,'/');if(slash)*slash=0;
  if(!*part||!strcmp(part,".")||!strcmp(part,"..")){close(fd);errno=EINVAL;return -1;}
  if(!slash){strcpy(name,part);return fd;}
  if(create&&mkdirat(fd,part,0700)&&errno!=EEXIST){int e=errno;close(fd);errno=e;return -1;}
  int next=openat(fd,part,O_RDONLY|O_DIRECTORY|O_NOFOLLOW|O_CLOEXEC);
  if(next<0){int e=errno;close(fd);errno=e;return -1;}
  /* Original trashbind marks each recreated path component too, so the
   * original trash-list walker can expose and restore its subtree. */
  if(create&&depth>=2){
   char record[96];
   if(fgetxattr(next,"user.trim-trashbin-record",record,sizeof(record))<0&&errno==ENODATA){
    int len=snprintf(record,sizeof(record),"u:%u,t:%lu",(unsigned)getuid(),(unsigned long)time(NULL));
    if(fsetxattr(next,"user.trim-trashbin-record",record,len,0)){int e=errno;close(next);close(fd);errno=e;return -1;}
   }
  }
  depth++;
  close(fd);fd=next;part=slash+1;
 }
}

int unlinkat(int dirfd,const char *path,int flags){
 if(dirfd!=-7125&&dirfd!=-7126)return syscall(SYS_unlinkat,dirfd,path,flags);
 if(!getuid()||getuid()!=geteuid()||(flags&~AT_REMOVEDIR)){errno=EOPNOTSUPP;return -1;}
 const char *p=path;if(*p=='/')p++;
 char prefix[64];snprintf(prefix,sizeof(prefix),"vol1/%u/",(unsigned)getuid());
 if(strncmp(p,prefix,strlen(prefix))){errno=EACCES;return -1;}
 p+=strlen(prefix);
 char rootpath[64];snprintf(rootpath,sizeof(rootpath),"/vol1/%u",(unsigned)getuid());
 int root=open(rootpath,O_RDONLY|O_DIRECTORY|O_NOFOLLOW|O_CLOEXEC);if(root<0)return -1;
 char name[PATH_MAX];int source=parent_fd(root,p,0,name);
 if(source<0){int e=errno;close(root);errno=e;return -1;}
 if(dirfd==-7125){int r=syscall(SYS_unlinkat,source,name,flags),e=errno;close(source);close(root);errno=e;return r;}
 /* A second delete inside the trash must never turn into permanent deletion. */
 if(!strncmp(p,".@#local/",9)){close(source);close(root);errno=EOPNOTSUPP;return -1;}
 struct stat st;
 if(fstatat(source,name,&st,AT_SYMLINK_NOFOLLOW)){int e=errno;close(source);close(root);errno=e;return -1;}
 if(!S_ISREG(st.st_mode)&&!S_ISDIR(st.st_mode)){close(source);close(root);errno=EOPNOTSUPP;return -1;}
 char targetpath[PATH_MAX],destname[PATH_MAX];
 if(snprintf(targetpath,sizeof(targetpath),".@#local/trash/%s",p)>=(int)sizeof(targetpath)){close(source);close(root);errno=ENAMETOOLONG;return -1;}
 int dest=parent_fd(root,targetpath,1,destname);
 if(dest<0){int e=errno;close(source);close(root);errno=e;return -1;}
 int object=openat(source,name,O_RDONLY|O_NOFOLLOW|O_CLOEXEC|O_NONBLOCK);
 if(object<0){int e=errno;close(dest);close(source);close(root);errno=e;return -1;}
 char record[96];int len=snprintf(record,sizeof(record),"u:%u,t:%lu",(unsigned)getuid(),(unsigned long)time(NULL));
 /* Verify xattr support before moving. Failed rename leaves the file intact. */
 if(fsetxattr(object,"user.trim-trashbin-record",record,len,0)){int e=errno;close(object);close(dest);close(source);close(root);errno=e;return -1;}
 int result=-1;char candidate[PATH_MAX];
 for(unsigned n=0;n<10000;n++){
  if(!n)strcpy(candidate,destname);
  else {
   char *dot=strrchr(destname,'.');
   int length=(dot&&dot!=destname)?snprintf(candidate,sizeof(candidate),"%.*s_%u%s",(int)(dot-destname),destname,n,dot):snprintf(candidate,sizeof(candidate),"%s_%u",destname,n);
   if(length>=(int)sizeof(candidate)){errno=ENAMETOOLONG;break;}
  }
  result=syscall(SYS_renameat2,source,name,dest,candidate,1 /* RENAME_NOREPLACE */);
  if(!result||errno!=EEXIST)break;
 }
 int saved=errno;close(object);close(dest);close(source);close(root);errno=saved;return result;
}
