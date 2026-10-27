package main

import (
	"fmt"
	"strings"
)

func protectBackup(dir string) {
	p := strings.ReplaceAll(dir, "'", "''")
	script := `$ErrorActionPreference='Stop'; $p='` + p + `'; $acl=Get-Acl -LiteralPath $p; $acl.SetAccessRuleProtection($true,$false); foreach($sid in @([Security.Principal.WindowsIdentity]::GetCurrent().User.Value,'S-1-5-18','S-1-5-32-544')) { $identity=New-Object Security.Principal.SecurityIdentifier($sid); $rule=New-Object Security.AccessControl.FileSystemAccessRule($identity,'FullControl','ContainerInherit,ObjectInherit','None','Allow'); $acl.AddAccessRule($rule) }; Set-Acl -LiteralPath $p -AclObject $acl`
	if output, err := powershell(script); err != nil {
		panic(fmt.Errorf("无法为本机私有备份设置访问权限: %w: %s", err, output))
	}
}
