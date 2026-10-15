n,t=map(int,input().split()); a=list(map(int,input().split())); l=0; total=0; ans=n+1
for r,x in enumerate(a):
    total+=x
    while total>=t:
        ans=min(ans,r-l+1); total-=a[l]; l+=1
print(0 if ans==n+1 else ans)
