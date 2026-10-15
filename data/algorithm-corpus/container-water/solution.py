n=int(input()); a=list(map(int,input().split())); l=0; r=n-1; ans=0
while l<r:
    ans=max(ans,(r-l)*min(a[l],a[r]))
    if a[l]<a[r]: l+=1
    else: r-=1
print(ans)
