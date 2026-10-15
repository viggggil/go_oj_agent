n,k=map(int,input().split()); a=list(map(int,input().split())); l=max(a); r=sum(a)
while l<r:
    m=(l+r)//2; cnt=1; s=0
    for x in a:
        if s+x>m: cnt+=1; s=0
        s+=x
    if cnt<=k: r=m
    else: l=m+1
print(l)
