n,t=map(int,input().split()); a=list(map(int,input().split())); l=0; r=n
while l<r:
    m=(l+r)//2
    if a[m]<t: l=m+1
    else: r=m
print(l+1 if l<n and a[l]==t else -1)
