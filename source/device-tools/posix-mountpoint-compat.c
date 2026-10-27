/* Linux mountpoint compatibility for the service-local POSIX experiment.
 * fnOS libnfile uses a private faccessat dirfd command on its own kernel.
 * This reports actual mountpoints using Linux mountinfo on older kernels.
 * It neither implements nor advertises TRIM ACL.
 */
#define _GNU_SOURCE
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <limits.h>
#include <errno.h>
int is_mountpoint(const char *path, int flags)
{
    char *canonical, line[16384], decoded[PATH_MAX];
    FILE *fp;
    int found=0, saved;
    (void)flags;
    canonical=realpath(path,NULL);
    if(!canonical) return 0;
    fp=fopen("/proc/self/mountinfo","re");
    if(!fp){saved=errno;free(canonical);errno=saved;return 0;}
    while(fgets(line,sizeof(line),fp)){
        char *state=NULL,*token=strtok_r(line," ",&state);
        int field=1;
        while(token && field<5){token=strtok_r(NULL," ",&state);field++;}
        if(token){
            size_t i=0,j=0;
            while(token[i] && j+1<sizeof(decoded)){
                if(token[i]=='\\' && token[i+1]>='0' && token[i+1]<='7' && token[i+2]>='0' && token[i+2]<='7' && token[i+3]>='0' && token[i+3]<='7'){
                    decoded[j++]=(token[i+1]-'0')*64+(token[i+2]-'0')*8+token[i+3]-'0';i+=4;
                }else decoded[j++]=token[i++];
            }
            decoded[j]=0;
            if(!strcmp(decoded,canonical)){found=1;break;}
        }
    }
    fclose(fp);free(canonical);return found;
}
#ifdef PROBE_MAIN
int main(int argc,char **argv){int i;for(i=1;i<argc;i++)printf("%s %d\n",argv[i],is_mountpoint(argv[i],0));return 0;}
#endif
